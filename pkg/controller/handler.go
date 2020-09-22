package controller

import (
	"errors"
	"strings"

	"github.com/traefik/mesh/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

type enqueueConfigWorkHandler struct {
	workQueue workqueue.RateLimitingInterface
}

// OnAdd is called when an object is added to the informers cache.
func (h *enqueueConfigWorkHandler) OnAdd(obj interface{}) {
	h.enqueueWork(obj)
}

// OnUpdate is called when an object is updated in the informers cache.
func (h *enqueueConfigWorkHandler) OnUpdate(oldObj interface{}, newObj interface{}) {
	if _, ok := newObj.(*corev1.Node); ok {
		return
	}

	oldObjMeta, okOld := oldObj.(metav1.Object)
	newObjMeta, okNew := newObj.(metav1.Object)

	// This is a resync event, no extra work is needed.
	if okOld && okNew && oldObjMeta.GetResourceVersion() == newObjMeta.GetResourceVersion() {
		return
	}

	h.enqueueWork(newObj)
}

// OnDelete is called when an object is removed from the informers cache.
func (h *enqueueConfigWorkHandler) OnDelete(obj interface{}) {
	h.enqueueWork(obj)
}

func (h *enqueueConfigWorkHandler) enqueueWork(obj interface{}) {
	switch r := obj.(type) {
	case *corev1.Service:
		h.workQueue.Add(buildEventKey(k8s.ServiceKind, r.Namespace, r.Name))
	case *corev1.Node:
		h.workQueue.Add(buildEventKey(k8s.NodeKind, "", r.Name))
	default:
		h.workQueue.Add(configRefreshKey)
	}
}

type enqueueProxyWorkHandler struct {
	workQueue workqueue.RateLimitingInterface
}

// OnAdd is called when an object is added to the informers cache.
func (h *enqueueProxyWorkHandler) OnAdd(obj interface{}) {
	h.enqueueWork(obj)
}

// OnUpdate is called when an object is updated in the informers cache.
func (h *enqueueProxyWorkHandler) OnUpdate(oldObj interface{}, newObj interface{}) {
	oldObjMeta, okOld := oldObj.(metav1.Object)
	newObjMeta, okNew := newObj.(metav1.Object)

	// This is a resync event, no extra work is needed.
	if okOld && okNew && oldObjMeta.GetResourceVersion() == newObjMeta.GetResourceVersion() {
		return
	}

	h.enqueueWork(newObj)
}

// OnDelete is called when an object is removed from the informers cache.
func (h *enqueueProxyWorkHandler) OnDelete(_ interface{}) {}

func (h *enqueueProxyWorkHandler) enqueueWork(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	h.workQueue.Add(buildEventKey(k8s.PodKind, pod.Namespace, pod.Name))
}

func parseEventKey(key string) (string, string, string, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return "", "", "", errors.New("invalid key")
	}

	return parts[0], parts[1], parts[2], nil
}

func buildEventKey(kind, namespace, name string) string {
	return strings.Join([]string{kind, namespace, name}, "/")
}
