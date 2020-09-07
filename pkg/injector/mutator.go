package injector

import (
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/traefik/mesh/pkg/annotations"
	"github.com/traefik/mesh/pkg/k8s"
	"gomodules.xyz/jsonpatch/v2"
	admission "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodMutator handles admission requests for Pods.
type PodMutator struct {
	namespace        string
	kubednsServiceIP string
	clusterDomain    string
	dnsImage         string
	filter           *k8s.ResourceFilter
	log              logrus.FieldLogger
}

// NewPodMutator creates a new PodMutator.
func NewPodMutator(log logrus.FieldLogger, clusterDomain, namespace string, ignoredNamespaces, watchNamespaces []string, kubednsServiceIP, dnsImage string) *PodMutator {
	filter := k8s.NewResourceFilter(
		k8s.WatchNamespaces(watchNamespaces...),
		k8s.IgnoreNamespaces(ignoredNamespaces...),
		k8s.IgnoreNamespaces(metav1.NamespaceSystem),
		k8s.IgnoreApps("maesh", "jaeger"),
	)

	return &PodMutator{
		namespace:        namespace,
		kubednsServiceIP: kubednsServiceIP,
		clusterDomain:    clusterDomain,
		dnsImage:         dnsImage,
		filter:           filter,
		log:              log,
	}
}

// Mutate handles pod admission request by injecting a sidecar DNS proxy and configuring the pod to
// do DNS resolution via this proxy.
func (m *PodMutator) Mutate(req *admission.AdmissionRequest) (*admission.AdmissionResponse, error) {
	resp := &admission.AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
	}

	var pod corev1.Pod

	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		return nil, fmt.Errorf("unable to unmarshal raw object: %w", err)
	}

	if !m.needsMutation(req, &pod) {
		m.log.Debugf("Admission request for %q (%q) in namespace %q: Skipped", pod.Name, pod.GenerateName, req.Namespace)

		return resp, nil
	}

	m.mutate(&pod)

	updatedBytes, err := json.Marshal(pod)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal updated pod: %w", err)
	}

	patch, err := jsonpatch.CreatePatch(req.Object.Raw, updatedBytes)
	if err != nil {
		return nil, err
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal patch: %w", err)
	}

	patchType := admission.PatchTypeJSONPatch

	resp.Patch = patchBytes
	resp.PatchType = &patchType

	m.log.Debugf("Admission request for %q (%q) in namespace %q: Injected", pod.Name, pod.GenerateName, req.Namespace)

	return resp, nil
}

func (m *PodMutator) needsMutation(req *admission.AdmissionRequest, pod *corev1.Pod) bool {
	// Copy the pod to provide missing pieces without altering permanently the resource.
	// For example, when a Pod is created by a deployment, its namespace and name are empty.
	podCopy := pod.DeepCopy()

	if pod.ObjectMeta.Namespace == "" {
		podCopy.ObjectMeta.Namespace = req.Namespace
	}
	if pod.ObjectMeta.Name == "" {
		podCopy.ObjectMeta.Name = podCopy.ObjectMeta.GenerateName
	}

	if m.filter.IsIgnored(podCopy) {
		return false
	}

	alreadyInjected, err := annotations.GetSidecarInjected(podCopy.Annotations)
	if err != nil && err != annotations.ErrNotFound {
		m.log.Errorf("Unexpected annotation value for %q in namespace %q: %v", podCopy.Name, podCopy.Namespace, err)

		return false
	}

	if alreadyInjected {
		return false
	}

	return true
}

func (m *PodMutator) mutate(pod *corev1.Pod) {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	annotations.SetSidecarInjected(pod.Annotations)

	pod.Spec.DNSPolicy = corev1.DNSNone
	pod.Spec.DNSConfig = &corev1.PodDNSConfig{
		Nameservers: []string{"127.0.0.1"},
		Searches: []string{
			"default.svc." + m.clusterDomain,
			"svc." + m.clusterDomain,
			m.clusterDomain,
		},
		Options: []corev1.PodDNSConfigOption{
			{
				Name:  "ndots",
				Value: stringPtr("5"),
			},
			{
				Name: "edns0",
			},
		},
	}

	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name:            "dns-proxy",
		Image:           m.dnsImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{
			"dns",
			"--upstream=" + m.kubednsServiceIP + ":53",
			"--loglevel=debug",
			"--namespace=" + m.namespace,
			"--clusterDomain=" + m.clusterDomain,
			"--node=$(TRAEFIK_MESH_NODE)",
		},
		Env: []corev1.EnvVar{
			{
				Name: "TRAEFIK_MESH_NODE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			},
		},
	})
}

func stringPtr(value string) *string {
	return &value
}
