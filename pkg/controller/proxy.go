package controller

import (
	"context"

	"github.com/sirupsen/logrus"
	"github.com/traefik/mesh/pkg/k8s"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/core/v1"
)

// ProxyManager manages proxies.
type ProxyManager struct {
	kubeClient  kubernetes.Interface
	proxyLister listers.PodLister

	logger logrus.FieldLogger
}

// Patch patches the given proxy by adding its node name into its labels.
func (m *ProxyManager) Patch(ctx context.Context, namespace, name string) error {
	proxy, err := m.proxyLister.Pods(namespace).Get(name)
	if kerrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if proxy.Spec.NodeName == "" || proxy.Labels[k8s.LabelNode] == proxy.Spec.NodeName {
		return nil
	}

	proxy.Labels[k8s.LabelNode] = proxy.Spec.NodeName

	m.logger.Debugf("Setting label node=%s on proxy %q in namespace %q...", proxy.Spec.NodeName, name, namespace)

	_, err = m.kubeClient.CoreV1().Pods(namespace).Update(ctx, proxy, metav1.UpdateOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}

	return nil
}
