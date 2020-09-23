package dns

import (
	"fmt"
	"regexp"
	"time"

	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
)

// Exchanger is capable of exchanging DNS messages with an upstream server.
type Exchanger interface {
	Exchange(m *dns.Msg, address string) (r *dns.Msg, rtt time.Duration, err error)
}

// Proxy is a DNS proxy (UDP) which rewrite questions targeting the traefik mesh domain into the shadow service replica
// corresponding to the given node.
type Proxy struct {
	dns.Server
	client Exchanger

	upstream string

	logger logrus.FieldLogger
}

// NewProxy creates a new Proxy.
func NewProxy(logger logrus.FieldLogger, port int, upstream, nodeName, clusterDomain, traefikMeshDomain, traefikMeshNamespace string) (*Proxy, error) {
	router := dns.NewServeMux()

	traefikMeshDomainFqdn := dns.Fqdn(traefikMeshDomain)
	clusterDomainFqdn := dns.Fqdn(clusterDomain)

	traefikMeshServiceName := fmt.Sprintf(`^([a-zA-Z0-9-_]*)\.([a-zA-Z0-9-_]*)\.%s$`,
		regexp.QuoteMeta(traefikMeshDomainFqdn),
	)
	traefikMeshServiceNameRe, err := regexp.Compile(traefikMeshServiceName)
	if err != nil {
		return nil, fmt.Errorf("unable to compile traefik mesh service name regexp: %w", err)
	}

	shadowServiceReplicaNameRepl := fmt.Sprintf("$1-6d61657368-$2-6d61657368-%s.%s.svc.%s", nodeName, traefikMeshNamespace, clusterDomainFqdn)

	proxy := &Proxy{
		Server: dns.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Net:          "udp",
			Handler:      router,
			ReadTimeout:  2 * time.Second,
			WriteTimeout: 2 * time.Second,
		},
		client: &dns.Client{
			Net:          "udp",
			DialTimeout:  2 * time.Second,
			ReadTimeout:  2 * time.Second,
			WriteTimeout: 2 * time.Second,
		},
		upstream: upstream,
		logger:   logger,
	}

	router.HandleFunc(traefikMeshDomainFqdn, proxy.rewrite(traefikMeshServiceNameRe, shadowServiceReplicaNameRepl))
	router.HandleFunc(".", proxy.forward)

	return proxy, nil
}

// rewrite rewrites the first question if its class is IN, type is either A or AAAA and name matches the given regexp.
// If the name matches it will be replaced by the given string. nameRepl can use groups captured in the nameRe.
func (p *Proxy) rewrite(nameRe *regexp.Regexp, nameRepl string) dns.HandlerFunc {
	return func(w dns.ResponseWriter, m *dns.Msg) {
		// Only the first question will be rewritten. Whilst the packet format technically supports having more than
		// one record in the question section (see §4.1.2 of RFC 1035), in practise nameservers don't support multiple
		// questions.
		question := m.Question[0]
		originalName := question.Name

		// Attempt a rewrite only if the class is IN and type either A or AAAA.
		if question.Qclass != dns.ClassINET || (question.Qtype != dns.TypeA && question.Qtype != dns.TypeAAAA) {
			p.forward(w, m)

			return
		}

		rewrittenName := nameRe.ReplaceAllString(originalName, nameRepl)
		m.Question[0].Name = rewrittenName

		if originalName != rewrittenName {
			p.logger.Debugf("Question %q has been rewritten into %q", originalName, rewrittenName)
		}

		r, _, err := p.client.Exchange(m, p.upstream)
		if err != nil {
			p.logger.Errorf("unable to forward message: %v", err)
			p.writeError(w, m, dns.RcodeServerFailure)

			return
		}

		r.Question[0].Name = originalName

		for _, answer := range r.Answer {
			if answer.Header().Name == rewrittenName {
				answer.Header().Name = originalName
			}
		}

		if err = w.WriteMsg(r); err != nil {
			p.logger.Errorf("unable to write response for %q: %v", r.Question[0].String(), err)
		}
	}
}

// forward forwards questions to the upstream server.
func (p *Proxy) forward(w dns.ResponseWriter, m *dns.Msg) {
	r, _, err := p.client.Exchange(m, p.upstream)
	if err != nil {
		p.logger.Errorf("unable to forward message: %v", err)
		p.writeError(w, m, dns.RcodeServerFailure)

		return
	}

	if err = w.WriteMsg(r); err != nil {
		p.logger.Errorf("unable to write response for %q: %v", r.Question[0].String(), err)
	}
}

func (p *Proxy) writeError(w dns.ResponseWriter, r *dns.Msg, rcode int) {
	m := &dns.Msg{}
	m.SetRcode(r, rcode)

	if err := w.WriteMsg(m); err != nil {
		p.logger.Errorf("unable to write error response for %q: %v", r.Question[0].String(), err)
	}
}
