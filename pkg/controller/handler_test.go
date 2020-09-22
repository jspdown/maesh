package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

func TestEnqueueConfigWorkHandler_OnAdd(t *testing.T) {
	workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	handler := &enqueueConfigWorkHandler{workQueue: workQueue}
	handler.OnAdd(&corev1.Pod{})

	assert.Equal(t, 1, workQueue.Len())

	currentKey, _ := workQueue.Get()

	assert.Equal(t, configRefreshKey, currentKey)
}

func TestEnqueueConfigWorkHandler_OnDelete(t *testing.T) {
	workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	handler := &enqueueConfigWorkHandler{workQueue: workQueue}
	handler.OnDelete(&corev1.Pod{})

	assert.Equal(t, 1, workQueue.Len())

	currentKey, _ := workQueue.Get()

	assert.Equal(t, configRefreshKey, currentKey)
}

func TestEnqueueConfigWorkHandler_OnUpdate(t *testing.T) {
	tests := []struct {
		desc        string
		oldObj      interface{}
		newObj      interface{}
		expectedLen int
	}{
		{
			desc: "should not enqueue if this is a re-sync event",
			oldObj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "foo"},
			},
			newObj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "foo"},
			},
			expectedLen: 0,
		},
		{
			desc: "should enqueue if this is not a re-sync event",
			oldObj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "foo"},
			},
			newObj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "bar"},
			},
			expectedLen: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

			handler := &enqueueConfigWorkHandler{workQueue: workQueue}
			handler.OnUpdate(test.oldObj, test.newObj)

			assert.Equal(t, test.expectedLen, workQueue.Len())
		})
	}
}

func TestEnqueueConfigWorkHandler_enqueueWork(t *testing.T) {
	tests := []struct {
		desc        string
		obj         interface{}
		expectedLen int
		expectedKey string
	}{
		{
			desc:        "should enqueue a refresh key if obj is not a service",
			obj:         &corev1.Endpoints{},
			expectedLen: 1,
			expectedKey: configRefreshKey,
		},
		{
			desc: "should enqueue a service key",
			obj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
			},
			expectedLen: 1,
			expectedKey: "Service/bar/foo",
		},
		{
			desc: "should enqueue a node key",
			obj: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			},
			expectedLen: 1,
			expectedKey: "Node//foo",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

			handler := &enqueueConfigWorkHandler{workQueue: workQueue}
			handler.enqueueWork(test.obj)

			assert.Equal(t, test.expectedLen, workQueue.Len())

			currentKey, _ := workQueue.Get()

			assert.Equal(t, test.expectedKey, currentKey)
		})
	}
}

func TestEnqueueProxyWorkHandler_OnAdd(t *testing.T) {
	workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	handler := &enqueueProxyWorkHandler{workQueue: workQueue}
	handler.OnAdd(&corev1.Pod{})

	assert.Equal(t, 1, workQueue.Len())
}

func TestEnqueueProxyWorkHandler_OnDelete(t *testing.T) {
	workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	handler := &enqueueProxyWorkHandler{workQueue: workQueue}
	handler.OnDelete(&corev1.Pod{})

	assert.Equal(t, 0, workQueue.Len())
}

func TestEnqueueProxyWorkHandler_OnUpdate(t *testing.T) {
	tests := []struct {
		desc        string
		oldObj      interface{}
		newObj      interface{}
		expectedLen int
	}{
		{
			desc: "should not enqueue if this is a re-sync event",
			oldObj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "foo"},
			},
			newObj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "foo"},
			},
			expectedLen: 0,
		},
		{
			desc: "should enqueue if this is not a re-sync event",
			oldObj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "foo"},
			},
			newObj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{ResourceVersion: "bar"},
			},
			expectedLen: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

			handler := &enqueueProxyWorkHandler{workQueue: workQueue}
			handler.OnUpdate(test.oldObj, test.newObj)

			assert.Equal(t, test.expectedLen, workQueue.Len())
		})
	}
}

func TestEnqueueProxyWorkHandler_enqueueWork(t *testing.T) {
	tests := []struct {
		desc        string
		obj         interface{}
		expectedLen int
		expectedKey string
	}{
		{
			desc:        "should do nothing if the object is not a pod",
			obj:         &corev1.Endpoints{},
			expectedLen: 0,
		},
		{
			desc: "should enqueue a key",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
			},
			expectedLen: 1,
			expectedKey: "Pod/bar/foo",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			workQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

			handler := &enqueueProxyWorkHandler{workQueue: workQueue}
			handler.enqueueWork(test.obj)

			assert.Equal(t, test.expectedLen, workQueue.Len())
			if test.expectedLen == 0 {
				return
			}

			currentKey, _ := workQueue.Get()

			assert.Equal(t, test.expectedKey, currentKey)
		})
	}
}
