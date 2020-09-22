package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/mesh/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	listers "k8s.io/client-go/listers/core/v1"
)

func TestProxyManager_PatchProxyNotFound(t *testing.T) {
	logger := logrus.New()
	client, proxyLister := newFakeProxyClient(t)

	m := &ProxyManager{
		kubeClient:  client,
		proxyLister: proxyLister,
		logger:      logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, m.Patch(ctx, "my-ns", "my-svc"))
}

func TestProxyManager_PatchUpdatesProxy(t *testing.T) {
	logger := logrus.New()

	proxy1 := newFakeProxyPod("my-ns", "proxy-1", "node-1")
	proxy2 := newFakeProxyPod("my-ns", "proxy-2", "node-1")

	client, proxyLister := newFakeProxyClient(t, proxy1, proxy2)

	m := &ProxyManager{
		kubeClient:  client,
		proxyLister: proxyLister,
		logger:      logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NoError(t, m.Patch(ctx, "my-ns", "proxy-1"))

	var err error

	proxy1, err = client.CoreV1().Pods("my-ns").Get(ctx, "proxy-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "node-1", proxy1.Labels[k8s.LabelNode])

	proxy2, err = client.CoreV1().Pods("my-ns").Get(ctx, "proxy-2", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, proxy2.Labels[k8s.LabelNode])
}

func newFakeProxyClient(t *testing.T, objects ...runtime.Object) (*fake.Clientset, listers.PodLister) {
	client := fake.NewSimpleClientset(objects...)

	informerFactory := informers.NewSharedInformerFactory(client, 5*time.Minute)
	proxyLister := informerFactory.Core().V1().Pods().Lister()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	informerFactory.Start(ctx.Done())

	for typ, ok := range informerFactory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			require.NoError(t, fmt.Errorf("timed out waiting for controller caches to sync: %s", typ))
		}
	}

	return client, proxyLister
}

func newFakeProxyPod(namespace, name, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				k8s.LabelComponent: k8s.ComponentProxy,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}
