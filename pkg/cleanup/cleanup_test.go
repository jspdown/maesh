package cleanup

import (
	"context"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/mesh/v2/pkg/dns"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCleanup_New(t *testing.T) {
	logger := logrus.New()

	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.DebugLevel)

	cleanup := NewCleanup(logger, fake.NewSimpleClientset())
	require.NotNil(t, cleanup)
}

func TestCleanup_RestoreDNSConfigWithCoreDNS(t *testing.T) {
	dnsRestorer := &dnsRestorerMock{
		provider: dns.CoreDNS,
	}

	cleanup := Cleanup{
		dns: dnsRestorer,
	}

	err := cleanup.RestoreDNSConfig(context.Background())
	require.NoError(t, err)

	assert.True(t, dnsRestorer.corednsRestored)
	assert.False(t, dnsRestorer.kubednsRestored)
}

func TestCleanup_RestoreDNSConfigWithKubeDNS(t *testing.T) {
	dnsRestorer := &dnsRestorerMock{
		provider: dns.KubeDNS,
	}

	cleanup := Cleanup{
		dns: dnsRestorer,
	}

	err := cleanup.RestoreDNSConfig(context.Background())
	require.NoError(t, err)

	assert.False(t, dnsRestorer.corednsRestored)
	assert.True(t, dnsRestorer.kubednsRestored)
}

func TestCleanup_RestoreDNSConfigWithUnknownDNSProvider(t *testing.T) {
	dnsRestorer := &dnsRestorerMock{
		provider: dns.UnknownDNS,
	}

	cleanup := Cleanup{
		dns: dnsRestorer,
	}

	err := cleanup.RestoreDNSConfig(context.Background())
	require.NoError(t, err)

	assert.False(t, dnsRestorer.corednsRestored)
	assert.False(t, dnsRestorer.kubednsRestored)
}

type dnsRestorerMock struct {
	provider        dns.Provider
	corednsRestored bool
	kubednsRestored bool
}

func (d *dnsRestorerMock) CheckDNSProvider(_ context.Context) (dns.Provider, error) {
	return d.provider, nil
}

func (d *dnsRestorerMock) RestoreCoreDNS(_ context.Context) error {
	d.corednsRestored = true

	return nil
}

func (d *dnsRestorerMock) RestoreKubeDNS(_ context.Context) error {
	d.kubednsRestored = true

	return nil
}
