package dns

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxy(t *testing.T) {
	tests := []struct {
		desc          string
		questionName  string
		questionClass uint16
		questionType  uint16
		expectedName  string
	}{
		{
			desc:          "Rewritten when name is a traefik mesh service with class is IN and type A",
			questionName:  "svc.my-ns.traefik.mesh.",
			questionClass: dns.ClassINET,
			questionType:  dns.TypeA,
			expectedName:  "svc-6d61657368-my-ns-6d61657368-node-1.traefik.svc.cluster.local.",
		},
		{
			desc:          "Rewritten when name is a traefik mesh service with class is IN and type AAAA",
			questionName:  "svc.my-ns.traefik.mesh.",
			questionClass: dns.ClassINET,
			questionType:  dns.TypeAAAA,
			expectedName:  "svc-6d61657368-my-ns-6d61657368-node-1.traefik.svc.cluster.local.",
		},
		{
			desc:          "Not rewritten when name is a traefik mesh service with class is not IN and type is A",
			questionName:  "svc.my-ns.traefik.mesh.",
			questionClass: dns.ClassCHAOS,
			questionType:  dns.TypeA,
			expectedName:  "svc.my-ns.traefik.mesh.",
		},
		{
			desc:          "Not rewritten when name is a traefik mesh service with class is IN and type is not A or AAAA",
			questionName:  "svc.my-ns.traefik.mesh.",
			questionClass: dns.ClassINET,
			questionType:  dns.TypeSRV,
			expectedName:  "svc.my-ns.traefik.mesh.",
		},
		{
			desc:          "Not rewritten when name is not a traefik mesh service with class is IN and type is A",
			questionName:  "svc.my-ns.svc.cluster.local.",
			questionClass: dns.ClassINET,
			questionType:  dns.TypeA,
			expectedName:  "svc.my-ns.svc.cluster.local.",
		},
		{
			desc:          "Not rewritten when name is not a traefik mesh service with class is IN and type is A",
			questionName:  "svc.my-ns.something.traefik.mesh.",
			questionClass: dns.ClassINET,
			questionType:  dns.TypeA,
			expectedName:  "svc.my-ns.something.traefik.mesh.",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			logger := logrus.New()

			var exchangeCalledCounter int

			wantUpstream := "upstream:53"

			wantMsg := &dns.Msg{}
			wantMsg.Question = append(wantMsg.Question, dns.Question{
				Name:   test.questionName,
				Qtype:  test.questionType,
				Qclass: test.questionClass,
			})

			wantRewittenMsg := wantMsg.Copy()
			wantRewittenMsg.Answer = nil
			wantRewittenMsg.Ns = nil
			wantRewittenMsg.Extra = nil
			wantRewittenMsg.Question[0].Name = test.expectedName

			wantReply := &dns.Msg{}
			wantReply.SetReply(wantMsg)

			rrHeader := dns.RR_Header{
				Name:   wantReply.Question[0].Name,
				Rrtype: test.questionType,
				Class:  test.questionClass,
				Ttl:    0,
			}

			if test.questionType == dns.TypeA {
				wantReply.Answer = append(wantReply.Answer, &dns.A{
					Hdr: rrHeader,
					A:   net.ParseIP("0.0.0.0"),
				})
			} else if test.questionType == dns.TypeAAAA {
				wantReply.Answer = append(wantReply.Answer, &dns.AAAA{
					Hdr:  rrHeader,
					AAAA: net.ParseIP("0:0:0:0:0:0:0:0"),
				})
			}

			exchanger := func(gotMsg *dns.Msg, gotUpstream string) (r *dns.Msg, rtt time.Duration, err error) {
				assert.Equal(t, wantRewittenMsg, gotMsg)
				assert.Equal(t, wantUpstream, gotUpstream)

				exchangeCalledCounter++

				return wantReply, 50 * time.Millisecond, nil
			}

			p, err := NewProxy(logger, 53, wantUpstream, "node-1", "cluster.local", "traefik.mesh", "traefik")
			require.NoError(t, err)

			p.client = fakeExchanger(exchanger)

			w := &responseWriter{}

			p.Server.Handler.ServeDNS(w, wantMsg)

			assert.Equal(t, 1, exchangeCalledCounter)
			assert.Equal(t, wantReply, w.gotReply)
		})
	}
}

type fakeExchanger func(m *dns.Msg, address string) (r *dns.Msg, rtt time.Duration, err error)

func (t fakeExchanger) Exchange(m *dns.Msg, address string) (r *dns.Msg, rtt time.Duration, err error) {
	return t(m, address)
}

type responseWriter struct {
	dns.ResponseWriter

	gotReply *dns.Msg
}

func (rw *responseWriter) WriteMsg(msg *dns.Msg) error {
	rw.gotReply = msg

	return nil
}
