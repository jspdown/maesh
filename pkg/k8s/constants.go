package k8s

import (
	"time"
)

const (
	// ResyncPeriod set the resync period.
	ResyncPeriod = 5 * time.Minute

	// TrafficSplitObjectKind is the name of an SMI object of kind TrafficSplit.
	TrafficSplitObjectKind = "TrafficSplit"
	// TrafficTargetObjectKind is the name of an SMI object of kind TrafficTarget.
	TrafficTargetObjectKind = "TrafficTarget"
	// HTTPRouteGroupObjectKind is the name of an SMI object of kind HTTPRouteGroup.
	HTTPRouteGroupObjectKind = "HTTPRouteGroup"
	// TCPRouteObjectKind is the name of an SMI object of kind TCPRoute.
	TCPRouteObjectKind = "TCPRoute"

	// ServiceKind is the name of a corev1.Service.
	ServiceKind = "Service"
	// PodKind is the name of a corev1.Pod.
	PodKind = "Pod"

	// CoreObjectKinds is a filter for objects to process by the core client.
	CoreObjectKinds = "Deployment|Endpoints|Service|Ingress|Secret|Namespace|Pod|ConfigMap"
	// AccessObjectKinds is a filter for objects to process by the access client.
	AccessObjectKinds = TrafficTargetObjectKind
	// SpecsObjectKinds is a filter for objects to process by the specs client.
	SpecsObjectKinds = HTTPRouteGroupObjectKind + "|" + TCPRouteObjectKind
	// SplitObjectKinds is a filter for objects to process by the split client.
	SplitObjectKinds = TrafficSplitObjectKind

	// LabelComponent is the name of the label for storing the component name.
	LabelComponent = "component"
	// LabelApp is the name of the label for storing the app name.
	LabelApp = "app"
	// LabelNode is the name of the label for storing the node for which the resource has been created.
	LabelNode = "node"

	// AppMaesh is the name of the app.
	AppMaesh = "maesh"

	// ComponentProxy is the component name of a mesh proxy.
	ComponentProxy = "maesh-mesh"
)
