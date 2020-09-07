package cmd

import (
	"os"
)

// TraefikMeshConfiguration wraps the static configuration and extra parameters.
type TraefikMeshConfiguration struct {
	ConfigFile       string   `description:"Configuration file to use. If specified all other flags are ignored." export:"true"`
	KubeConfig       string   `description:"Path to a kubeconfig. Only required if out-of-cluster." export:"true"`
	MasterURL        string   `description:"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster." export:"true"`
	LogLevel         string   `description:"The log level." export:"true"`
	LogFormat        string   `description:"The log format." export:"true"`
	Debug            bool     `description:"Debug mode, deprecated, use --loglevel=debug instead." export:"true"`
	ACL              bool     `description:"Enable ACL mode." export:"true"`
	SMI              bool     `description:"Enable SMI operation, deprecated, use --acl instead." export:"true"`
	DefaultMode      string   `description:"Default mode for mesh services." export:"true"`
	Namespace        string   `description:"The namespace that Traefik Mesh is installed in." export:"true"`
	WatchNamespaces  []string `description:"Namespaces to watch." export:"true"`
	IgnoreNamespaces []string `description:"Namespaces to ignore." export:"true"`
	APIPort          int32    `description:"API port for the controller." export:"true"`
	APIHost          string   `description:"API host for the controller to bind to." export:"true"`
	LimitHTTPPort    int32    `description:"Number of HTTP ports allocated." export:"true"`
	LimitTCPPort     int32    `description:"Number of TCP ports allocated." export:"true"`
	LimitUDPPort     int32    `description:"Number of UDP ports allocated." export:"true"`
}

// NewTraefikMeshConfiguration creates a TraefikMeshConfiguration with default values.
func NewTraefikMeshConfiguration() *TraefikMeshConfiguration {
	return &TraefikMeshConfiguration{
		ConfigFile:    "",
		KubeConfig:    os.Getenv("KUBECONFIG"),
		LogLevel:      "error",
		LogFormat:     "common",
		Debug:         false,
		ACL:           false,
		SMI:           false,
		DefaultMode:   "http",
		Namespace:     "maesh",
		APIPort:       9000,
		APIHost:       "",
		LimitHTTPPort: 10,
		LimitTCPPort:  25,
		LimitUDPPort:  25,
	}
}

// PrepareConfiguration holds the configuration to prepare the cluster.
type PrepareConfiguration struct {
	ConfigFile    string `description:"Configuration file to use. If specified all other flags are ignored." export:"true"`
	KubeConfig    string `description:"Path to a kubeconfig. Only required if out-of-cluster." export:"true"`
	MasterURL     string `description:"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster." export:"true"`
	LogLevel      string `description:"The log level." export:"true"`
	LogFormat     string `description:"The log format." export:"true"`
	Debug         bool   `description:"Debug mode, deprecated, use --loglevel=debug instead." export:"true"`
	Namespace     string `description:"The namespace that Traefik Mesh is installed in." export:"true"`
	ClusterDomain string `description:"Your internal K8s cluster domain." export:"true"`
	SMI           bool   `description:"Enable SMI operation, deprecated, use --acl instead." export:"true"`
	ACL           bool   `description:"Enable ACL mode." export:"true"`
}

// NewPrepareConfiguration creates a PrepareConfiguration with default values.
func NewPrepareConfiguration() *PrepareConfiguration {
	return &PrepareConfiguration{
		KubeConfig:    os.Getenv("KUBECONFIG"),
		LogLevel:      "error",
		LogFormat:     "common",
		Debug:         false,
		Namespace:     "maesh",
		ClusterDomain: "cluster.local",
		SMI:           false,
	}
}

// CleanupConfiguration holds the configuration for the cleanup command.
type CleanupConfiguration struct {
	ConfigFile string `description:"Configuration file to use. If specified all other flags are ignored." export:"true"`
	KubeConfig string `description:"Path to a kubeconfig. Only required if out-of-cluster." export:"true"`
	MasterURL  string `description:"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster." export:"true"`
	Namespace  string `description:"The namespace that Traefik Mesh is installed in." export:"true"`
	LogLevel   string `description:"The log level." export:"true"`
	LogFormat  string `description:"The log format." export:"true"`
}

// NewCleanupConfiguration creates CleanupConfiguration.
func NewCleanupConfiguration() *CleanupConfiguration {
	return &CleanupConfiguration{
		KubeConfig: os.Getenv("KUBECONFIG"),
		Namespace:  "maesh",
		LogLevel:   "error",
		LogFormat:  "common",
	}
}

// DNSConfiguration holds the configuration for the dns command.
type DNSConfiguration struct {
	LogLevel      string `description:"The log level." export:"true"`
	LogFormat     string `description:"The log format." export:"true"`
	Port          int    `description:"Port on which the proxy is exposed." export:"true"`
	Upstream      string `description:"Destination endpoint to forward to." export:"true"`
	Node          string `description:"Name of the node" export:"true"`
	ClusterDomain string `description:"The internal K8s cluster domain." export:"true"`
	Namespace     string `description:"The namespace that Traefik Mesh is installed in." export:"true"`
}

// NewDNSConfiguration creates DNSConfiguration.
func NewDNSConfiguration() *DNSConfiguration {
	return &DNSConfiguration{
		LogLevel:      "error",
		LogFormat:     "common",
		Port:          53,
		ClusterDomain: "cluster.local",
		Namespace:     "maesh",
	}
}

// InjectConfiguration holds the configuration for the inject command.
type InjectConfiguration struct {
	Port             int32    `description:"Webhook port." export:"true"`
	Host             string   `description:"Webhook host to bind to." export:"true"`
	ResolvConf       string   `description:"DNS resolver configuration." export:"true"`
	TLSCertFile      string   `description:"TLS certificate path." export:"true"`
	TLSKeyFile       string   `description:"TLS key path." export:"true"`
	WatchNamespaces  []string `description:"Namespaces to watch." export:"true"`
	IgnoreNamespaces []string `description:"Namespaces to ignore." export:"true"`
	ClusterDomain    string   `description:"Your internal K8s cluster domain." export:"true"`
	Namespace        string   `description:"The namespace that Traefik Mesh is installed in." export:"true"`
	LogLevel         string   `description:"The log level." export:"true"`
	LogFormat        string   `description:"The log format." export:"true"`
	DNSImage         string   `description:"The DNS proxy image." export:"true"`
}

// NewInjectConfiguration creates InjectConfiguration.
func NewInjectConfiguration() *InjectConfiguration {
	return &InjectConfiguration{
		Port:          443,
		ClusterDomain: "cluster.local",
		Namespace:     "maesh",
		LogLevel:      "error",
		LogFormat:     "common",
		ResolvConf:    "/etc/resolv.conf",
		DNSImage:      "traefik/mesh:latest",
	}
}
