package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/mesh/pkg/annotations"
	"github.com/traefik/mesh/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	listers "k8s.io/client-go/listers/core/v1"
)

const (
	testNamespace          = "test"
	testDefaultTrafficType = annotations.ServiceTypeHTTP
)

func TestShadowServiceManager_LoadPortMapping(t *testing.T) {
	logger := logrus.New()

	svc1 := newFakeService("default", "svc-1", map[int]int{8000: 80}, annotations.ServiceTypeTCP)
	svc2 := newFakeService("default", "svc-2", map[int]int{8000: 80}, annotations.ServiceTypeTCP)

	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")

	shadowSvc1 := newFakeShadowService(t, svc1, map[int]int{8000: 5000})
	shadowSvc2 := newFakeShadowService(t, svc2, map[int]int{8000: 5001})

	// Add an incompatible port: UDP in a TCP.
	shadowSvc1.Spec.Ports = append(shadowSvc1.Spec.Ports, corev1.ServicePort{
		Name:       "incompatible-port",
		Protocol:   corev1.ProtocolUDP,
		Port:       9000,
		TargetPort: intstr.FromInt(5002),
	})

	shadowSvc1Node1 := newFakeShadowServiceReplica(t, shadowSvc1, node1)
	shadowSvc1Node2 := newFakeShadowServiceReplica(t, shadowSvc1, node2)
	shadowSvc2Node1 := newFakeShadowServiceReplica(t, shadowSvc2, node1)

	tcpPortMapper := &portMappingMock{
		t: t,
		setCalledWith: []portMapping{
			{namespace: svc1.Namespace, name: svc1.Name, fromPort: 8000, toPort: 5000},
			{namespace: svc2.Namespace, name: svc2.Name, fromPort: 8000, toPort: 5001},
		},
	}

	_, svcLister, _ := newFakeK8sClient(t,
		node1, node2,
		svc1, svc2,
		shadowSvc1, shadowSvc2,
		shadowSvc1Node1, shadowSvc1Node2, shadowSvc2Node1)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		serviceLister:      svcLister,
		tcpStateTable:      tcpPortMapper,
		logger:             logger,
	}

	assert.NoError(t, mgr.LoadPortMapping())

	assert.Equal(t, 2, tcpPortMapper.setCounter)
}

// TestShadowServiceManager_SyncServiceHandlesUnknownTrafficTypes tests the case where a service is updated with an
// invalid traffic type. It ensures that both shadow service creation and update are handled correctly.
func TestShadowServiceManager_SyncServiceHandlesUnknownTrafficTypes(t *testing.T) {
	logger := logrus.New()

	// Create a service and simulate an update on the ports from 8000 to 9000 and with an invalid traffic type.
	svc := newFakeService("default", "svc", map[int]int{9000: 80}, "pigeon")
	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")

	shadowSvc := newFakeShadowService(t, svc, map[int]int{8000: 5000})

	// Create a shadow service replica just for the first node.
	shadowSvcNode1 := newFakeShadowServiceReplica(t, shadowSvc, node1)

	client, svcLister, nodeLister := newFakeK8sClient(t,
		node1, node2,
		svc, shadowSvc, shadowSvcNode1)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncService(ctx, svc.Namespace, svc.Name))

	// Make sure the shadow service stays intact.
	syncedShadowSvc := getShadowService(t, client, shadowSvc.Name)
	assert.Equal(t, shadowSvc.Annotations, syncedShadowSvc.Annotations)
	assert.Equal(t, shadowSvc.Spec.Ports, syncedShadowSvc.Spec.Ports)

	// Make sure the existing shadow service replica stays intact.
	syncedShadowSvcNode1 := getShadowService(t, client, shadowSvcNode1.Name)
	assert.Equal(t, shadowSvcNode1.Annotations, syncedShadowSvcNode1.Annotations)
	assert.Equal(t, shadowSvcNode1.Spec.Ports, syncedShadowSvcNode1.Spec.Ports)

	// Make sure the missing shadow service has not been created.
	shadowSvcNode2Name, err := getShadowServiceReplicaName(svc.Namespace, svc.Name, node2.Name)
	require.NoError(t, err)
	assert.False(t, checkShadowServiceExists(t, client, shadowSvcNode2Name))
}

// TestShadowServiceManager_SyncServiceHandlesServiceNameSizeLimit tests the case where a service is created and its
// shadow service name is too long (> 63 chars). It ensures no ports are leaked in this situation.
func TestShadowServiceManager_SyncServiceHandlesServiceNameSizeLimit(t *testing.T) {
	logger := logrus.New()

	// Create a service with a very long name.
	name := "very-very-very-very-very-very-very-very-very-very-very-very-very-very-long-name"
	svc := newFakeService("default", name, map[int]int{8000: 80}, annotations.ServiceTypeHTTP)

	node1 := newFakeNode("node1")

	client, svcLister, nodeLister := newFakeK8sClient(t, node1, svc)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncService(ctx, svc.Namespace, svc.Name))
}

// TestShadowServiceManager_SyncServiceUpdateShadowServicesAndHandleTrafficTypeChanges tests the case a service has
// been updated and its traffic type has changed.
func TestShadowServiceManager_SyncServiceUpdateShadowServicesAndHandleTrafficTypeChanges(t *testing.T) {
	logger := logrus.New()

	// Create a service and simulate an update on the ports and traffic type.
	svc := newFakeService("default", "svc", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)
	updatedSvc := newFakeService("default", "svc", map[int]int{9000: 1010}, annotations.ServiceTypeUDP)

	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")

	shadowSvc := newFakeShadowService(t, svc, map[int]int{8000: 5000})

	// Create shadow service just for the first node.
	shadowSvcNode1 := newFakeShadowServiceReplica(t, shadowSvc, node1)

	httpPortMapper := &portMappingMock{
		t: t,
		removeCalledWith: []portMapping{
			{namespace: svc.Namespace, name: svc.Name, fromPort: 8000, toPort: 5000},
		},
	}
	udpPortMapper := &portMappingMock{
		t: t,
		addCalledWith: []portMapping{
			{namespace: svc.Namespace, name: svc.Name, fromPort: 9000, toPort: 10000},
		},
	}

	client, svcLister, nodeLister := newFakeK8sClient(t,
		node1, node2,
		updatedSvc, shadowSvc, shadowSvcNode1)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		httpStateTable:     httpPortMapper,
		udpStateTable:      udpPortMapper,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncService(ctx, svc.Namespace, svc.Name))

	shadowSvcNode2Name, err := getShadowServiceReplicaName(svc.Namespace, svc.Name, node2.Name)
	require.NoError(t, err)

	// Make sure the existing shadow service has been updated.
	shadowSvcs := []*corev1.Service{
		getShadowService(t, client, shadowSvc.Name),
		getShadowService(t, client, shadowSvcNode1.Name),
		getShadowService(t, client, shadowSvcNode2Name),
	}
	for _, shadowSvc := range shadowSvcs {
		trafficType, err := annotations.GetTrafficType("", shadowSvc.Annotations)
		require.NoError(t, err)
		assert.Equal(t, annotations.ServiceTypeUDP, trafficType)

		assert.Len(t, shadowSvc.Spec.Ports, 1)
		assert.Equal(t, corev1.ProtocolUDP, shadowSvc.Spec.Ports[0].Protocol)
		assert.Equal(t, int32(9000), shadowSvc.Spec.Ports[0].Port)
		assert.Equal(t, int32(10000), shadowSvc.Spec.Ports[0].TargetPort.IntVal)
	}

	assert.Equal(t, 1, httpPortMapper.removeCounter)
	assert.Equal(t, 1, udpPortMapper.addCounter)
}

// TestShadowServiceManager_SyncServiceUpsertShadowServices tests the case where a service has been updated and
// some shadow services are missing. We are expecting to see an update of the existing shadow services and the
// creation of the missing ones.
func TestShadowServiceManager_SyncServiceUpsertShadowServices(t *testing.T) {
	logger := logrus.New()

	// Create a service and simulate an update on the ports.
	svc := newFakeService("default", "svc", map[int]int{8000: 80, 9001: 8081}, annotations.ServiceTypeHTTP)
	updatedSvc := newFakeService("default", "svc", map[int]int{9000: 8080, 9001: 8081}, annotations.ServiceTypeHTTP)

	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")

	shadowSvc := newFakeShadowService(t, svc, map[int]int{8000: 5000, 9001: 5001})

	// Create a shadow service just for the first node.
	shadowSvcNode1 := newFakeShadowServiceReplica(t, shadowSvc, node1)

	httpPortMapper := &portMappingMock{
		t: t,
		removeCalledWith: []portMapping{
			{namespace: svc.Namespace, name: svc.Name, fromPort: 8000, toPort: 5000},
		},
		addCalledWith: []portMapping{
			{namespace: svc.Namespace, name: svc.Name, fromPort: 9000, toPort: 5000},
			{namespace: svc.Namespace, name: svc.Name, fromPort: 9001, toPort: 5001},
		},
	}

	client, svcLister, nodeLister := newFakeK8sClient(t,
		node1, node2,
		updatedSvc, shadowSvc, shadowSvcNode1)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		httpStateTable:     httpPortMapper,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncService(ctx, svc.Namespace, svc.Name))

	shadowSvcNode2Name, err := getShadowServiceReplicaName(svc.Namespace, svc.Name, node2.Name)
	require.NoError(t, err)

	// Make sure the existing shadow service has been updated.
	shadowSvcs := []*corev1.Service{
		getShadowService(t, client, shadowSvc.Name),
		getShadowService(t, client, shadowSvcNode1.Name),
		getShadowService(t, client, shadowSvcNode2Name),
	}
	for _, shadowSvc := range shadowSvcs {
		assert.Len(t, shadowSvc.Spec.Ports, 2)
		assert.Equal(t, corev1.ProtocolTCP, shadowSvc.Spec.Ports[0].Protocol)
		assert.Equal(t, int32(9000), shadowSvc.Spec.Ports[0].Port)
		assert.Equal(t, int32(5000), shadowSvc.Spec.Ports[0].TargetPort.IntVal)
		assert.Equal(t, int32(9001), shadowSvc.Spec.Ports[1].Port)
		assert.Equal(t, int32(5001), shadowSvc.Spec.Ports[1].TargetPort.IntVal)
	}

	assert.Equal(t, 1, httpPortMapper.removeCounter)
	assert.Equal(t, 2, httpPortMapper.addCounter)
}

// TestShadowServiceManager_SyncServiceDeleteShadowServices checks the case where the given service has been removed
// and there are still some shadow services left.
func TestShadowServiceManager_SyncServiceDeleteShadowServices(t *testing.T) {
	logger := logrus.New()

	// Simulate a service that have been removed.
	svc := newFakeService("default", "svc", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)

	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")
	node3 := newFakeNode("node-3")

	shadowSvc := newFakeShadowService(t, svc, map[int]int{8000: 5000})

	// Create shadow services for the first two nodes.
	shadowSvcNode1 := newFakeShadowServiceReplica(t, shadowSvc, node1)
	shadowSvcNode2 := newFakeShadowServiceReplica(t, shadowSvc, node2)

	httpPortMapper := &portMappingMock{
		t: t,
		removeCalledWith: []portMapping{
			{namespace: svc.Namespace, name: svc.Name, fromPort: 8000, toPort: 5000},
		},
	}

	client, svcLister, nodeLister := newFakeK8sClient(t,
		node1, node2, node3,
		shadowSvc, shadowSvcNode1, shadowSvcNode2)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		httpStateTable:     httpPortMapper,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncService(ctx, svc.Namespace, svc.Name))

	// Check if the shadow service have been removed. Replica will be removed automatically by Kubernetes
	// garbage collector.
	assert.False(t, checkShadowServiceExists(t, client, shadowSvc.Name))

	assert.Equal(t, 1, httpPortMapper.removeCounter)
}

// TestShadowServiceManager_SyncNodeHandlesNodeCreation tests the case where a new node is added. In this test we
// make sure the new shadow services are created. We also make sure that a left over shadow service, resulting from
// a re-added node is not an issue.
func TestShadowServiceManager_SyncNodeHandlesNodeCreation(t *testing.T) {
	logger := logrus.New()

	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")

	svc := newFakeService("default", "svc", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)

	// Create a service in an ignored namespace to make sure ignored resources are taken into account.
	svcIgnored := newFakeService("ignored", "svc", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)

	shadowSvc := newFakeShadowService(t, svc, map[int]int{8000: 5000})
	shadowSvcIgnored := newFakeShadowService(t, svcIgnored, map[int]int{8000: 5001})

	shadowSvcNode1 := newFakeShadowServiceReplica(t, shadowSvc, node1)

	client, svcLister, nodeLister := newFakeK8sClient(t,
		node1, node2,
		svc, svcIgnored, shadowSvcIgnored, shadowSvc, shadowSvcNode1)

	resourceFilter := k8s.NewResourceFilter(
		k8s.IgnoreNamespaces("ignored"),
		k8s.IgnoreApps("maesh"),
	)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		resourceFilter:     resourceFilter,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncNode(ctx, node2.Name))

	shadowSvcNode2Name, err := getShadowServiceReplicaName(svc.Namespace, svc.Name, node2.Name)
	require.NoError(t, err)

	shadowSvcs := []*corev1.Service{
		getShadowService(t, client, shadowSvc.Name),
		getShadowService(t, client, shadowSvcNode1.Name),
		getShadowService(t, client, shadowSvcNode2Name),
	}

	for _, svc := range shadowSvcs {
		assert.Len(t, svc.Spec.Ports, 1)
		assert.Equal(t, corev1.ProtocolTCP, svc.Spec.Ports[0].Protocol)
		assert.Equal(t, int32(8000), svc.Spec.Ports[0].Port)
		assert.Equal(t, int32(5000), svc.Spec.Ports[0].TargetPort.IntVal)
	}

	// Make sure ignored resources are left intact.
	ignoredShadowSvcNode2Name, err := getShadowServiceReplicaName(svcIgnored.Namespace, svcIgnored.Name, node2.Name)
	require.NoError(t, err)

	assert.False(t, checkShadowServiceExists(t, client, ignoredShadowSvcNode2Name))
}

// TestShadowServiceManager_SyncNodeHandlesNodeDeletion tests the cases where the given node has been removed. In this
// test we make sure associated shadow services are being removed. We also make sure a missing shadow service is not
// an issue.
func TestShadowServiceManager_SyncNodeHandlesNodeDeletion(t *testing.T) {
	logger := logrus.New()

	// Create a node an simulate it has been removed.
	node1 := newFakeNode("node-1")
	node2 := newFakeNode("node-2")

	svc1 := newFakeService("default", "svc-1", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)
	svc2 := newFakeService("default", "svc-2", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)

	// Create a service in an ignored namespace to make sure ignored resources are taken into account.
	svcIgnored := newFakeService("ignored", "svc-4", map[int]int{8000: 80}, annotations.ServiceTypeHTTP)

	shadowSvc1 := newFakeShadowService(t, svc1, map[int]int{8000: 5000})
	shadowSvc1Node1 := newFakeShadowServiceReplica(t, shadowSvc1, node1)
	shadowSvc1Node2 := newFakeShadowServiceReplica(t, shadowSvc1, node2)

	shadowSvc2 := newFakeShadowService(t, svc2, map[int]int{8000: 5001})
	shadowSvc2Node1 := newFakeShadowServiceReplica(t, shadowSvc2, node1)
	shadowSvc2Node2 := newFakeShadowServiceReplica(t, shadowSvc2, node2)

	shadowSvcIgnored := newFakeShadowService(t, svcIgnored, map[int]int{8000: 5002})
	shadowSvcIgnoredNode1 := newFakeShadowServiceReplica(t, shadowSvcIgnored, node1)
	shadowSvcIgnoredNode2 := newFakeShadowServiceReplica(t, shadowSvcIgnored, node2)

	client, svcLister, nodeLister := newFakeK8sClient(t,
		node1,
		svc1, shadowSvc1, shadowSvc1Node1, shadowSvc1Node2,
		svc2, shadowSvc2, shadowSvc2Node1, shadowSvc2Node2,
		svcIgnored, shadowSvcIgnored, shadowSvcIgnoredNode1, shadowSvcIgnoredNode2)

	resourceFilter := k8s.NewResourceFilter(
		k8s.IgnoreNamespaces("ignored"),
		k8s.IgnoreApps("maesh"),
	)

	mgr := ShadowServiceManager{
		namespace:          testNamespace,
		defaultTrafficType: testDefaultTrafficType,
		resourceFilter:     resourceFilter,
		kubeClient:         client,
		nodeLister:         nodeLister,
		serviceLister:      svcLister,
		logger:             logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, mgr.SyncNode(ctx, node2.Name))

	// Check that only shadow service replicas of node2 have been removed. And not those
	// that are ignored.
	deletedShadowSvcs := []string{
		shadowSvc1Node2.Name,
		shadowSvc2Node2.Name,
	}

	for _, name := range deletedShadowSvcs {
		assert.False(t, checkShadowServiceExists(t, client, name))
	}

	notDeletedShadowSvcs := []string{
		shadowSvc1.Name,
		shadowSvc2.Name,
		shadowSvc1Node1.Name,
		shadowSvc2Node1.Name,
		shadowSvcIgnored.Name,
		shadowSvcIgnoredNode1.Name,
		shadowSvcIgnoredNode2.Name,
	}

	for _, name := range notDeletedShadowSvcs {
		assert.True(t, checkShadowServiceExists(t, client, name))
	}
}

func newFakeK8sClient(t *testing.T, objects ...runtime.Object) (*fake.Clientset, listers.ServiceLister, listers.NodeLister) {
	client := fake.NewSimpleClientset(objects...)

	informerFactory := informers.NewSharedInformerFactory(client, 5*time.Minute)
	svcLister := informerFactory.Core().V1().Services().Lister()
	nodeLister := informerFactory.Core().V1().Nodes().Lister()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	informerFactory.Start(ctx.Done())

	for typ, ok := range informerFactory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			require.NoError(t, fmt.Errorf("timed out waiting for controller caches to sync: %s", typ))
		}
	}

	return client, svcLister, nodeLister
}

func getShadowService(t *testing.T, client kubernetes.Interface, name string) *corev1.Service {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	svc, err := client.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)

	return svc
}

func checkServiceExists(t *testing.T, client kubernetes.Interface, namespace, name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return false
	}

	require.NoError(t, err)

	return true
}

func checkShadowServiceExists(t *testing.T, client kubernetes.Interface, name string) bool {
	return checkServiceExists(t, client, testNamespace, name)
}

func newFakeNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newFakeService(namespace, name string, ports map[int]int, trafficType string) *corev1.Service {
	var svcPorts []corev1.ServicePort

	protocol := corev1.ProtocolTCP
	if trafficType == annotations.ServiceTypeUDP {
		protocol = corev1.ProtocolUDP
	}

	for port, targetPort := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       fmt.Sprintf("port-%d", port),
			Protocol:   protocol,
			Port:       int32(port),
			TargetPort: intstr.FromInt(targetPort),
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			Ports: svcPorts,
		},
	}

	if trafficType != "" {
		annotations.SetTrafficType(trafficType, svc.Annotations)
	}

	return svc
}

func newFakeShadowService(t *testing.T, svc *corev1.Service, ports map[int]int) *corev1.Service {
	var svcPorts []corev1.ServicePort

	name, err := getShadowServiceName(svc.Namespace, svc.Name)
	require.NoError(t, err)

	trafficType, _ := annotations.GetTrafficType(testDefaultTrafficType, svc.Annotations)

	protocol := corev1.ProtocolTCP
	if trafficType == annotations.ServiceTypeUDP {
		protocol = corev1.ProtocolUDP
	}

	for port, targetPort := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       fmt.Sprintf("port-%d", port),
			Protocol:   protocol,
			Port:       int32(port),
			TargetPort: intstr.FromInt(targetPort),
		})
	}

	shadowSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
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
			Ports: svcPorts,
		},
	}

	annotations.SetTrafficType(trafficType, shadowSvc.Annotations)

	return shadowSvc
}

func newFakeShadowServiceReplica(t *testing.T, shadowSvc *corev1.Service, node *corev1.Node) *corev1.Service {
	shadowSvcReplica := shadowSvc.DeepCopy()

	namespace := shadowSvc.Labels[k8s.LabelServiceNamespace]
	name := shadowSvc.Labels[k8s.LabelServiceName]

	shadowSvcReplicaName, err := getShadowServiceReplicaName(namespace, name, node.Name)
	require.NoError(t, err)

	shadowSvcReplica.Name = shadowSvcReplicaName
	shadowSvcReplica.Labels[k8s.LabelComponent] = k8s.ComponentShadowServiceReplica
	shadowSvcReplica.Labels[k8s.LabelNode] = node.Name
	shadowSvcReplica.Spec.Selector[k8s.LabelNode] = node.Name
	shadowSvcReplica.OwnerReferences = []metav1.OwnerReference{
		*metav1.NewControllerRef(shadowSvc, corev1.SchemeGroupVersion.WithKind(shadowSvc.Kind)),
	}

	return shadowSvcReplica
}

type portMapping struct {
	namespace string
	name      string
	fromPort  int32
	toPort    int32
}

type portMappingMock struct {
	t *testing.T

	setCalledWith    []portMapping
	addCalledWith    []portMapping
	findCalledWith   []portMapping
	removeCalledWith []portMapping

	setCounter    int
	addCounter    int
	findCounter   int
	removeCounter int
}

func (m *portMappingMock) Set(namespace, name string, fromPort, toPort int32) error {
	if m.setCalledWith == nil {
		assert.FailNowf(m.t, "Set has been called", "%s/%s %d-%d", name, namespace, fromPort, toPort)
	}

	if m.setCounter < len(m.setCalledWith) {
		callArgs := m.setCalledWith[m.setCounter]
		m.setCounter++

		if namespace == callArgs.namespace && name == callArgs.name && fromPort == callArgs.fromPort && toPort == callArgs.toPort {
			return nil
		}
	}

	assert.FailNowf(m.t, "unexpected call to Set", "%s/%s %d->%d", name, namespace, fromPort, toPort)

	return errors.New("fail")
}

func (m *portMappingMock) Add(namespace, name string, fromPort int32) (int32, error) {
	if m.addCalledWith == nil {
		assert.FailNowf(m.t, "Add has been called", "%s/%s %d", name, namespace, fromPort)
	}

	if m.addCounter < len(m.addCalledWith) {
		callArgs := m.addCalledWith[m.addCounter]
		m.addCounter++

		if namespace == callArgs.namespace && name == callArgs.name && fromPort == callArgs.fromPort {
			return callArgs.toPort, nil
		}
	}

	assert.FailNowf(m.t, "unexpected call to Add", "%s/%s %d", name, namespace, fromPort)

	return 0, errors.New("fail")
}

func (m *portMappingMock) Find(namespace, name string, fromPort int32) (int32, bool) {
	if m.findCalledWith == nil {
		assert.FailNowf(m.t, "Find has been called", "%s/%s %d", name, namespace, fromPort)
	}

	if m.findCounter < len(m.findCalledWith) {
		callArgs := m.findCalledWith[m.findCounter]
		m.findCounter++

		if namespace == callArgs.namespace && name == callArgs.name && fromPort == callArgs.fromPort {
			return callArgs.toPort, true
		}
	}

	assert.FailNowf(m.t, "unexpected call to Find", "%s/%s %d", name, namespace, fromPort)

	return 0, false
}

func (m *portMappingMock) Remove(namespace, name string, fromPort int32) (int32, bool) {
	if m.removeCalledWith == nil {
		assert.FailNowf(m.t, "Remove has been called", "%s/%s %d", name, namespace, fromPort)
	}

	if m.removeCounter < len(m.removeCalledWith) {
		callArgs := m.removeCalledWith[m.removeCounter]
		m.removeCounter++

		if namespace == callArgs.namespace && name == callArgs.name && fromPort == callArgs.fromPort {
			return callArgs.toPort, true
		}
	}

	assert.FailNowf(m.t, "unexpected call to Remove", "%s/%s %d", name, namespace, fromPort)

	return 0, false
}
