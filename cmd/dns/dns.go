package dns

import (
	"context"
	"fmt"

	"github.com/traefik/mesh/cmd"
	"github.com/traefik/mesh/pkg/dns"
	"github.com/traefik/paerser/cli"
)

const traefikMeshDomain = "traefik.mesh"

// NewCmd builds a new DNS command.
func NewCmd(cfg *cmd.DNSConfiguration, loaders []cli.ResourceLoader) *cli.Command {
	return &cli.Command{
		Name:          "dns",
		Description:   `DNS proxy`,
		Configuration: cfg,
		Run: func(_ []string) error {
			return dnsCommand(cfg)
		},
		Resources: loaders,
	}
}

func dnsCommand(cfg *cmd.DNSConfiguration) error {
	ctx := cmd.ContextWithSignal(context.Background())

	logger, err := cmd.NewLogger(cfg.LogFormat, cfg.LogLevel, false)
	if err != nil {
		return fmt.Errorf("could not create logger: %w", err)
	}

	if cfg.Upstream == "" {
		return fmt.Errorf("upstream address is required")
	}

	if cfg.Node == "" {
		return fmt.Errorf("node is required")
	}

	logger.Debug("Starting DNS proxy...")
	logger.Debug("Port: ", cfg.Port)
	logger.Debug("Upstream: ", cfg.Upstream)
	logger.Debug("Node: ", cfg.Node)
	logger.Debug("Namespace: ", cfg.Namespace)
	logger.Debug("ClusterDomain: ", cfg.ClusterDomain)

	proxyStoppedCh := make(chan struct{})

	proxy, err := dns.NewProxy(logger, cfg.Port, cfg.Upstream, cfg.Node, cfg.ClusterDomain, traefikMeshDomain, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("unable to create proxy: %w", err)
	}

	go func() {
		if err := proxy.ListenAndServe(); err != nil {
			logger.Errorf("Proxy has stopped unexpectedly: %v", err)
		}

		proxyStoppedCh <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		if err := proxy.Shutdown(); err != nil {
			logger.Errorf("unable to stop proxy gracefully: %v", err)
		}

		<-proxyStoppedCh
	case <-proxyStoppedCh:
	}

	return nil
}
