package inject

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/miekg/dns"
	"github.com/traefik/mesh/cmd"
	"github.com/traefik/mesh/pkg/injector"
	"github.com/traefik/paerser/cli"
)

// NewCmd builds a new Inject command.
func NewCmd(cfg *cmd.InjectConfiguration, loaders []cli.ResourceLoader) *cli.Command {
	return &cli.Command{
		Name:          "inject",
		Description:   `Inject command.`,
		Configuration: cfg,
		Run: func(_ []string) error {
			return injectCommand(cfg)
		},
		Resources: loaders,
	}
}

func injectCommand(cfg *cmd.InjectConfiguration) error {
	ctx := cmd.ContextWithSignal(context.Background())

	log, err := cmd.NewLogger(cfg.LogFormat, cfg.LogLevel, false)
	if err != nil {
		return fmt.Errorf("could not create logger: %w", err)
	}

	log.Debug("Starting injector...")

	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("unable to load key pair: %w", err)
	}

	clusterDNS, err := getClusterDNS(cfg.ResolvConf)
	if err != nil {
		return fmt.Errorf("unable to get cluster DNS service IP: %w", err)
	}

	log.Debugf("Cluster DNS Service IP: %s", clusterDNS)

	mutator := injector.NewPodMutator(log,
		cfg.ClusterDomain,
		cfg.Namespace,
		cfg.IgnoreNamespaces,
		cfg.WatchNamespaces,
		clusterDNS,
		cfg.DNSImage)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	api := injector.NewAPI(log, addr, cert, mutator)

	return api.Start(ctx)
}

func getClusterDNS(resolvConf string) (string, error) {
	cfg, err := dns.ClientConfigFromFile(resolvConf)
	if err != nil {
		return "", err
	}

	if len(cfg.Servers) == 0 {
		return "", errors.New("server not found")
	}

	return cfg.Servers[0], nil
}
