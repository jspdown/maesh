package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/traefik/mesh/pkg/annotations"
	"github.com/traefik/mesh/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/core/v1"
)

// PortMapper is capable of storing and retrieving a port mapping for a given service.
type PortMapper interface {
	Find(namespace, name string, port int32) (int32, bool)
	Add(namespace, name string, port int32) (int32, error)
	Set(namespace, name string, port, targetPort int32) error
	Remove(namespace, name string, port int32) (int32, bool)
}

// ShadowServiceManager keeps shadow services in sync with user services.
// A shadow service is a copy of a user service on which the traffic is routed to a proxy. For each living user-service
// it exists has many shadow services are there are nodes in the cluster. Each of which targets the proxy of its node.
type ShadowServiceManager struct {
	namespace          string
	defaultTrafficType string
	resourceFilter     *k8s.ResourceFilter

	kubeClient    kubernetes.Interface
	nodeLister    listers.NodeLister
	serviceLister listers.ServiceLister

	httpStateTable PortMapper
	tcpStateTable  PortMapper
	udpStateTable  PortMapper

	logger logrus.FieldLogger
}

// LoadPortMapping loads the port mapping of existing shadow services into the different port mappers.
func (m *ShadowServiceManager) LoadPortMapping() error {
	shadowSvcs, err := m.getShadowServices()
	if err != nil {
		return fmt.Errorf("unable to list shadow services: %w", err)
	}

	for _, shadowSvc := range shadowSvcs {
		trafficType, err := annotations.GetTrafficType("", shadowSvc.Annotations)
		if err == nil && trafficType == "" {
			err = errors.New("traffic-type has been removed")
		}

		if err != nil {
			m.logger.Errorf("Unable to load port mapping of shadow service %q: %v", shadowSvc.Name, err)
			continue
		}

		m.loadShadowServicePorts(shadowSvc, trafficType)
	}

	return nil
}

// SyncService synchronizes shadow services with the given service.
func (m *ShadowServiceManager) SyncService(ctx context.Context, namespace, name string) error {
	m.logger.Debugf("Syncing service %q in namespace %q...", name, namespace)

	shadowSvcName, err := getShadowServiceName(namespace, name)
	if err != nil {
		m.logger.Errorf("Unable to sync service %q in namespace %q: %v", name, namespace, err)
		return nil
	}

	svc, err := m.serviceLister.Services(namespace).Get(name)
	if kerrors.IsNotFound(err) {
		return m.deleteShadowService(ctx, namespace, name, shadowSvcName)
	}

	if err != nil {
		return err
	}

	return m.upsertShadowService(ctx, svc, shadowSvcName)
}

// SyncNode synchronizes shadow services with the given node.
func (m *ShadowServiceManager) SyncNode(ctx context.Context, nodeName string) error {
	m.logger.Debugf("Syncing node %q...", nodeName)

	_, err := m.nodeLister.Get(nodeName)
	if kerrors.IsNotFound(err) {
		return m.deleteShadowServiceReplicas(ctx, nodeName)
	}

	if err != nil {
		return err
	}

	return m.upsertShadowServiceReplicas(ctx, nodeName)
}

func (m *ShadowServiceManager) deleteShadowServiceReplicas(ctx context.Context, nodeName string) error {
	svcs, err := m.getUserServices()
	if err != nil {
		return err
	}

	for _, svc := range svcs {
		shadowSvcReplicaName, err := getShadowServiceReplicaName(svc.Namespace, svc.Name, nodeName)
		if err != nil {
			m.logger.Errorf("Unable to delete shadow service for service %q in namespace %q and node %q: %v", svc.Namespace, svc.Name, nodeName, err)
			continue
		}

		m.logger.Debugf("Deleting shadow service replica %q...", shadowSvcReplicaName)

		err = m.kubeClient.CoreV1().Services(m.namespace).Delete(ctx, shadowSvcReplicaName, metav1.DeleteOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (m *ShadowServiceManager) upsertShadowServiceReplicas(ctx context.Context, nodeName string) error {
	svcs, err := m.getUserServices()
	if err != nil {
		return err
	}

	for _, svc := range svcs {
		shadowSvcName, err := getShadowServiceName(svc.Namespace, svc.Name)
		if err != nil {
			m.logger.Errorf("Unable to create or update shadow service for service %q in namespace %q and node %q: %v", svc.Namespace, svc.Name, nodeName, err)
			continue
		}

		shadowSvc, err := m.serviceLister.Services(m.namespace).Get(shadowSvcName)
		if kerrors.IsNotFound(err) {
			continue
		}

		if err != nil {
			return err
		}

		if err = m.replicateShadowService(ctx, svc, shadowSvc, nodeName); err != nil {
			return err
		}
	}

	return nil
}

func (m *ShadowServiceManager) deleteShadowService(ctx context.Context, namespace, name, shadowSvcName string) error {
	shadowSvc, err := m.serviceLister.Services(m.namespace).Get(shadowSvcName)
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}

	m.logger.Debugf("Deleting shadow service %q...", shadowSvcName)

	trafficType, err := annotations.GetTrafficType("", shadowSvc.Annotations)
	if err == nil && trafficType == "" {
		err = errors.New("traffic-type has been removed")
	}

	if err != nil {
		m.logger.Errorf("Unable to delete shadow service for service %q in namespace %q: %v", name, namespace, err)
		return nil
	}

	for _, sp := range shadowSvc.Spec.Ports {
		if err = m.unmapPort(namespace, name, trafficType, sp.Port); err != nil {
			m.logger.Errorf("Unable to unmap port %d of service %q in namespace %q: %v", sp.Port, name, namespace)
		}
	}

	// As this shadow service is the owner of the node-replicated shadow services, they will be removed implicitly.
	err = m.kubeClient.CoreV1().Services(m.namespace).Delete(ctx, shadowSvcName, metav1.DeleteOptions{})
	if kerrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (m *ShadowServiceManager) upsertShadowService(ctx context.Context, svc *corev1.Service, shadowSvcName string) error {
	var (
		err         error
		trafficType string
		shadowSvc   *corev1.Service
	)

	trafficType, err = annotations.GetTrafficType(m.defaultTrafficType, svc.Annotations)
	if err != nil {
		m.logger.Errorf("Unable to create or update shadow services for service %q in namespace %q: %v", svc.Name, svc.Namespace, err)
		return nil
	}

	shadowSvc, err = m.serviceLister.Services(m.namespace).Get(shadowSvcName)
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}

	if kerrors.IsNotFound(err) {
		shadowSvc, err = m.createShadowService(ctx, svc, shadowSvcName, trafficType)
	} else {
		shadowSvc, err = m.updateShadowService(ctx, svc, shadowSvc, trafficType)
	}

	if err != nil {
		return err
	}

	nodes, err := m.nodeLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if err := m.replicateShadowService(ctx, svc, shadowSvc, node.Name); err != nil {
			return err
		}
	}

	return nil
}

func (m *ShadowServiceManager) createShadowService(ctx context.Context, svc *corev1.Service, shadowSvcName, trafficType string) (*corev1.Service, error) {
	m.logger.Debugf("Creating shadow service %q...", shadowSvcName)

	ports := m.getServicePorts(svc, trafficType)
	if len(ports) == 0 {
		ports = []corev1.ServicePort{buildUnresolvablePort()}
	}

	shadowSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shadowSvcName,
			Namespace: m.namespace,
			Labels: map[string]string{
				k8s.LabelApp:              k8s.AppMaesh,
				k8s.LabelComponent:        k8s.ComponentShadowService,
				k8s.LabelServiceNamespace: svc.Namespace,
				k8s.LabelServiceName:      svc.Name,
			},
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				k8s.LabelComponent: k8s.ComponentProxy,
			},
			Ports: ports,
		},
	}

	annotations.SetTrafficType(trafficType, shadowSvc.Annotations)

	var err error

	shadowSvc, err = m.kubeClient.CoreV1().Services(m.namespace).Create(ctx, shadowSvc, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return nil, err
	}

	return shadowSvc, nil
}

func (m *ShadowServiceManager) updateShadowService(ctx context.Context, svc, shadowSvc *corev1.Service, trafficType string) (*corev1.Service, error) {
	m.logger.Debugf("Updating shadow service %q...", shadowSvc.Name)

	m.cleanupShadowServicePorts(svc, shadowSvc, trafficType)

	ports := m.getServicePorts(svc, trafficType)
	if len(ports) == 0 {
		ports = []corev1.ServicePort{buildUnresolvablePort()}
	}

	shadowSvc.Spec.Ports = ports

	annotations.SetTrafficType(trafficType, shadowSvc.Annotations)

	return m.kubeClient.CoreV1().Services(m.namespace).Update(ctx, shadowSvc, metav1.UpdateOptions{})
}

func (m *ShadowServiceManager) replicateShadowService(ctx context.Context, svc, shadowSvc *corev1.Service, nodeName string) error {
	shadowSvcReplicaName, err := getShadowServiceReplicaName(svc.Namespace, svc.Name, nodeName)
	if err != nil {
		m.logger.Errorf("Unable to replicate shadow service %q on node %q: %v", shadowSvc.Name, nodeName, err)
		return nil
	}

	shadowSvcReplica, err := m.serviceLister.Services(m.namespace).Get(shadowSvcReplicaName)
	if kerrors.IsNotFound(err) {
		m.logger.Debugf("Creating shadow service replica %q...", shadowSvcReplicaName)

		shadowSvcReplica = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shadowSvcReplicaName,
				Namespace: m.namespace,
				Labels: map[string]string{
					k8s.LabelApp:              k8s.AppMaesh,
					k8s.LabelComponent:        k8s.ComponentShadowServiceReplica,
					k8s.LabelServiceNamespace: svc.Namespace,
					k8s.LabelServiceName:      svc.Name,
					k8s.LabelNode:             nodeName,
				},
				Annotations: shadowSvc.Annotations,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(shadowSvc, schema.GroupVersionKind{
						Group:   corev1.SchemeGroupVersion.Group,
						Version: corev1.SchemeGroupVersion.Version,
						Kind:    "Service",
					}),
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					k8s.LabelComponent: k8s.ComponentProxy,
					k8s.LabelNode:      nodeName,
				},
				Ports: shadowSvc.Spec.Ports,
			},
		}

		_, err = m.kubeClient.CoreV1().Services(m.namespace).Create(ctx, shadowSvcReplica, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			return err
		}

		return nil
	}

	m.logger.Debugf("Updating shadow service replica %q...", shadowSvcReplicaName)

	shadowSvcReplica.Annotations = shadowSvc.Annotations
	shadowSvcReplica.Spec.Ports = shadowSvc.Spec.Ports

	_, err = m.kubeClient.CoreV1().Services(m.namespace).Update(ctx, shadowSvcReplica, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// cleanupShadowServicePorts unmap ports that have changed since the last update of the service.
func (m *ShadowServiceManager) cleanupShadowServicePorts(svc, shadowSvc *corev1.Service, trafficType string) {
	oldTrafficType, err := annotations.GetTrafficType("", shadowSvc.Annotations)
	if err == nil && oldTrafficType == "" {
		err = errors.New("traffic-type has been removed")
	}

	if err != nil {
		m.logger.Errorf("Unable to clean up ports for shadow service %q: %v", shadowSvc.Name, err)
		return
	}

	var oldPorts []corev1.ServicePort

	// Release ports which have changed since the last update. This operation has to be done before mapping new
	// ports as the number of target ports available is limited.
	if oldTrafficType != trafficType {
		// All ports have to be released if the traffic-type has changed.
		oldPorts = shadowSvc.Spec.Ports
	} else {
		oldPorts = getRemovedOrUpdatedPorts(shadowSvc.Spec.Ports, svc.Spec.Ports)
	}

	for _, sp := range oldPorts {
		if err := m.unmapPort(svc.Namespace, svc.Name, oldTrafficType, sp.Port); err != nil {
			m.logger.Errorf("Unable to unmap port %d of service %q in namespace %q: %v", sp.Port, svc.Name, svc.Namespace)
		}
	}
}

// getServicePorts returns the ports of the given user service, mapped with port opened on the proxy.
func (m *ShadowServiceManager) getServicePorts(svc *corev1.Service, trafficType string) []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, sp := range svc.Spec.Ports {
		if !isPortCompatible(trafficType, sp) {
			m.logger.Warnf("Unsupported port type %q on %q service %q in namespace %q, skipping port %d", sp.Protocol, trafficType, svc.Name, svc.Namespace, sp.Port)
			continue
		}

		targetPort, err := m.mapPort(svc.Name, svc.Namespace, trafficType, sp.Port)
		if err != nil {
			m.logger.Errorf("Unable to map port %d for %q service %q in namespace %q: %v", sp.Port, trafficType, svc.Name, svc.Namespace, err)
			continue
		}

		ports = append(ports, corev1.ServicePort{
			Name:       sp.Name,
			Port:       sp.Port,
			Protocol:   sp.Protocol,
			TargetPort: intstr.FromInt(int(targetPort)),
		})
	}

	return ports
}

// loadShadowServicePorts loads the port mapping of the given shadow service into the different port mappers.
func (m *ShadowServiceManager) loadShadowServicePorts(shadowSvc *corev1.Service, trafficType string) {
	namespace := shadowSvc.Labels[k8s.LabelServiceNamespace]
	name := shadowSvc.Labels[k8s.LabelServiceName]

	for _, sp := range shadowSvc.Spec.Ports {
		if !isPortCompatible(trafficType, sp) {
			m.logger.Warnf("Unsupported port type %q on %q service %q in namespace %q, skipping port %d", sp.Protocol, trafficType, shadowSvc.Name, shadowSvc.Namespace, sp.Port)
			continue
		}

		if err := m.setPort(name, namespace, trafficType, sp.Port, sp.TargetPort.IntVal); err != nil {
			m.logger.Errorf("Unable to load port %d for %q service %q in namespace %q: %v", sp.Port, trafficType, shadowSvc.Name, shadowSvc.Namespace, err)
			continue
		}
	}
}

// mapPort maps the given port to a port on the proxy, if not already done.
func (m *ShadowServiceManager) setPort(name, namespace, trafficType string, port, mappedPort int32) error {
	var stateTable PortMapper

	switch trafficType {
	case annotations.ServiceTypeHTTP:
		stateTable = m.httpStateTable
	case annotations.ServiceTypeTCP:
		stateTable = m.tcpStateTable
	case annotations.ServiceTypeUDP:
		stateTable = m.udpStateTable
	default:
		return fmt.Errorf("unknown traffic type %q", trafficType)
	}

	if err := stateTable.Set(namespace, name, port, mappedPort); err != nil {
		return err
	}

	m.logger.Debugf("Port %d of service %q in namespace %q has been loaded and is mapped to port %d", port, name, namespace, mappedPort)

	return nil
}

// mapPort maps the given port to a port on the proxy, if not already done.
func (m *ShadowServiceManager) mapPort(name, namespace, trafficType string, port int32) (int32, error) {
	var stateTable PortMapper

	switch trafficType {
	case annotations.ServiceTypeHTTP:
		stateTable = m.httpStateTable
	case annotations.ServiceTypeTCP:
		stateTable = m.tcpStateTable
	case annotations.ServiceTypeUDP:
		stateTable = m.udpStateTable
	default:
		return 0, fmt.Errorf("unknown traffic type %q", trafficType)
	}

	mappedPort, err := stateTable.Add(namespace, name, port)
	if err != nil {
		return 0, err
	}

	m.logger.Debugf("Port %d of service %q in namespace %q has been mapped to port %d", port, name, namespace, mappedPort)

	return mappedPort, nil
}

// unmapPort releases the port on the proxy associated with the given port. This released port can then be
// remapped  later on. Port releasing is delegated to the different port mappers, following the given traffic type.
func (m *ShadowServiceManager) unmapPort(namespace, name, trafficType string, port int32) error {
	var stateTable PortMapper

	switch trafficType {
	case annotations.ServiceTypeHTTP:
		stateTable = m.httpStateTable
	case annotations.ServiceTypeTCP:
		stateTable = m.tcpStateTable
	case annotations.ServiceTypeUDP:
		stateTable = m.udpStateTable
	default:
		return fmt.Errorf("unknown traffic type %q", trafficType)
	}

	if mappedPort, ok := stateTable.Remove(namespace, name, port); ok {
		m.logger.Debugf("Port %d of service %q in namespace %q has been unmapped to port %d", port, name, namespace, mappedPort)
	}

	return nil
}

// getUserServices returns all non-ignored services created by the user.
func (m *ShadowServiceManager) getUserServices() ([]*corev1.Service, error) {
	var svcs []*corev1.Service

	allSvcs, err := m.serviceLister.List(labels.Everything())
	if err != nil {
		return []*corev1.Service{}, err
	}

	for _, svc := range allSvcs {
		if !m.resourceFilter.IsIgnored(svc) {
			svcs = append(svcs, svc)
		}
	}

	return svcs, nil
}

// getUserServices returns all shadow services.
func (m *ShadowServiceManager) getShadowServices() ([]*corev1.Service, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			k8s.LabelApp:       k8s.AppMaesh,
			k8s.LabelComponent: k8s.ComponentShadowService,
		},
	})
	if err != nil {
		return []*corev1.Service{}, err
	}

	shadowSvcs, err := m.serviceLister.Services(m.namespace).List(selector)
	if err != nil {
		return []*corev1.Service{}, err
	}

	return shadowSvcs, nil
}

// buildUnresolvablePort builds a service port with a fake port. This fake port can be used as a placeholder when a service
// doesn't have any compatible ports.
func buildUnresolvablePort() corev1.ServicePort {
	return corev1.ServicePort{
		Name:     "unresolvable-port",
		Protocol: corev1.ProtocolTCP,
		Port:     1666,
	}
}

// getRemovedOrUpdatedPorts returns the list of ports which have been removed or updated in the newPorts slice.
// New ports won't be returned.
func getRemovedOrUpdatedPorts(oldPorts, newPorts []corev1.ServicePort) []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, oldPort := range oldPorts {
		var found bool

		for _, newPort := range newPorts {
			if oldPort.Port == newPort.Port && oldPort.Protocol == newPort.Protocol {
				found = true

				break
			}
		}

		if !found {
			ports = append(ports, oldPort)
		}
	}

	return ports
}

// isPortCompatible checks if the given port is compatible with the given traffic type.
func isPortCompatible(trafficType string, sp corev1.ServicePort) bool {
	switch trafficType {
	case annotations.ServiceTypeUDP:
		return sp.Protocol == corev1.ProtocolUDP
	case annotations.ServiceTypeTCP, annotations.ServiceTypeHTTP:
		return sp.Protocol == corev1.ProtocolTCP
	default:
		return false
	}
}

func getShadowServiceName(namespace, name string) (string, error) {
	shadowSvcName := fmt.Sprintf("%s-6d61657368-%s", name, namespace)

	if err := checkServiceName(shadowSvcName); err != nil {
		return "", fmt.Errorf("invalid shadow service name: %w", err)
	}

	return shadowSvcName, nil
}

func getShadowServiceReplicaName(namespace, name, node string) (string, error) {
	replicateShadowSvcName := fmt.Sprintf("%s-6d61657368-%s-6d61657368-%s", name, namespace, node)

	if err := checkServiceName(replicateShadowSvcName); err != nil {
		return "", fmt.Errorf("invalid shadow service name: %w", err)
	}

	return replicateShadowSvcName, nil
}

func checkServiceName(name string) error {
	errs := validation.IsDNS1123Label(name)
	if len(errs) != 0 {
		return errors.New(strings.Join(errs, ","))
	}

	return nil
}
