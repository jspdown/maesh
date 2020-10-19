package cleanup

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/traefik/mesh/v2/pkg/dns"
	"k8s.io/client-go/kubernetes"
)

// DNSRestorer is capable of restoring DNS configurations.
type DNSRestorer interface {
	CheckDNSProvider(ctx context.Context) (dns.Provider, error)
	RestoreCoreDNS(ctx context.Context) error
	RestoreKubeDNS(ctx context.Context) error
}

// Cleanup holds the clients for the various resource controllers.
type Cleanup struct {
	dns DNSRestorer
}

// NewCleanup returns an initialized cleanup object.
func NewCleanup(logger logrus.FieldLogger, kubeClient kubernetes.Interface) *Cleanup {
	dnsClient := dns.NewClient(logger, kubeClient)

	return &Cleanup{
		dns: dnsClient,
	}
}

// RestoreDNSConfig restores the configmap and restarts the DNS pods.
func (c *Cleanup) RestoreDNSConfig(ctx context.Context) error {
	provider, err := c.dns.CheckDNSProvider(ctx)
	if err != nil {
		return err
	}

	// Restore configmaps based on DNS provider.
	switch provider {
	case dns.CoreDNS:
		if err := c.dns.RestoreCoreDNS(ctx); err != nil {
			return fmt.Errorf("unable to restore CoreDNS: %w", err)
		}
	case dns.KubeDNS:
		if err := c.dns.RestoreKubeDNS(ctx); err != nil {
			return fmt.Errorf("unable to restore KubeDNS: %w", err)
		}
	}

	return nil
}
