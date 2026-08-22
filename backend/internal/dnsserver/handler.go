package dnsserver

import (
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/queryobs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// Handler answers the narrow question set of design D7 and refuses everything
// else. It is the whole DNS behaviour of the service; the listeners in server.go
// only carry bytes to it.
type Handler struct {
	zone    string // fqdn, lowercase
	view    *zoneview.View
	metrics *obs.Metrics
	// Counts answered queries per alias so the console can say whether the CA
	// ever came to look. Optional: nil disables the counting entirely, and
	// Observe on a nil counter is a no-op, so no call site needs a guard.
	queries *queryobs.Counter

	challengeTTL uint32
	soa          *dns.SOA // also the authority-section record of every negative answer
	ns           []dns.RR
}

// NewHandler builds the handler from configuration. The SOA and NS records are
// fixed at construction: the SOA serial deliberately does not change when
// challenge records change (spec: "zone housekeeping records are served").
func NewHandler(cfg *config.Config, view *zoneview.View, metrics *obs.Metrics, queries *queryobs.Counter) *Handler {
	zone := dns.Fqdn(strings.ToLower(cfg.Zone))
	negTTL := secs(cfg.SOAMinimum)

	h := &Handler{
		zone:         zone,
		view:         view,
		metrics:      metrics,
		queries:      queries,
		challengeTTL: secs(cfg.ChallengeTTL),
		soa: &dns.SOA{
			Hdr:     dns.RR_Header{Name: zone, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: negTTL},
			Ns:      dns.Fqdn(strings.ToLower(cfg.SOAMName)),
			Mbox:    dns.Fqdn(strings.ToLower(cfg.SOARName)),
			Serial:  cfg.SOASerial,
			Refresh: secs(cfg.SOARefresh),
			Retry:   secs(cfg.SOARetry),
			Expire:  secs(cfg.SOAExpire),
			Minttl:  negTTL,
		},
	}
	for _, name := range cfg.NSNames {
		h.ns = append(h.ns, &dns.NS{
			Hdr: dns.RR_Header{Name: zone, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: negTTL},
			Ns:  dns.Fqdn(strings.ToLower(name)),
		})
	}
	return h
}

func secs(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	return uint32(d / time.Second)
}

func (h *Handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r) // copies RD; leaves RA unset, which is what an authoritative-only server owes.

	// A message that did not parse never reaches here: miekg answers FORMERR
	// itself whenever the header parses and the body does not, and drops the
	// datagram when even the header is garbage (any reply would be amplifiable).
	// What can still arrive is a well-formed message we do not serve.
	switch {
	case r.Opcode != dns.OpcodeQuery:
		// Not in the D7 table; REFUSED is the answer that neither amplifies nor
		// claims anything about a name.
		m.Rcode = dns.RcodeRefused
	case len(r.Question) != 1:
		m.Rcode = dns.RcodeFormatError
	default:
		h.answer(m, r.Question[0])
	}

	h.setEDNS(m, r)
	m.Truncate(h.maxSize(w, r))

	if h.metrics != nil {
		qtype := "NONE"
		if len(r.Question) == 1 {
			qtype = dns.Type(r.Question[0].Qtype).String()
		}
		h.metrics.DNSQueries.WithLabelValues(qtype, dns.RcodeToString[m.Rcode]).Inc()
	}

	// A write failure is a dead client, not our problem; the listener keeps serving.
	_ = w.WriteMsg(m)
}

// answer implements the D7 decision table.
func (h *Handler) answer(m *dns.Msg, q dns.Question) {
	qname := strings.ToLower(dns.Fqdn(q.Name))

	if !dns.IsSubDomain(h.zone, qname) {
		// We are authoritative for one zone and nothing else. Not NXDOMAIN
		// (we know nothing about that name) and never recursion.
		m.Rcode = dns.RcodeRefused
		return
	}

	m.Authoritative = true

	if qname == h.zone {
		switch q.Qtype {
		case dns.TypeSOA:
			m.Answer = []dns.RR{h.soa}
		case dns.TypeNS:
			m.Answer = h.ns
		default:
			h.negative(m, dns.RcodeSuccess)
		}
		return
	}

	// One label under the apex, or it cannot be an alias.
	label := strings.TrimSuffix(qname, "."+h.zone)
	if strings.Contains(label, ".") {
		h.negative(m, dns.RcodeNameError)
		return
	}

	values, exists := h.view.Lookup(label)
	switch {
	case !exists:
		// The name does not exist. NXDOMAIN, and the SOA carries how long a
		// resolver may remember that.
		h.negative(m, dns.RcodeNameError)
	case q.Qtype != dns.TypeTXT, len(values) == 0:
		// The name exists with no records of this type. NOERROR with an empty
		// answer — emphatically not NXDOMAIN, which resolvers cache against
		// every type at the name.
		h.negative(m, dns.RcodeSuccess)
	default:
		for _, v := range values {
			m.Answer = append(m.Answer, &dns.TXT{
				Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: h.challengeTTL},
				Txt: []string{v},
			})
		}
		// Someone actually asked for a live value — the observation the console
		// reports as "queried". Counted here and nowhere else: an empty answer
		// or an NXDOMAIN says nothing about a validation being checked, and a
		// query that arrives after cleanup is not evidence of anything either.
		//
		// An in-memory increment, deliberately. See package queryobs: this path
		// must not touch the datastore.
		h.queries.Observe(label)
	}
}

// negative sets an empty-answer response with the SOA in AUTHORITY, which is
// where the negative caching duration comes from.
func (h *Handler) negative(m *dns.Msg, rcode int) {
	m.Rcode = rcode
	m.Ns = []dns.RR{h.soa}
}

// setEDNS echoes an OPT record when the client sent one. A response to a query
// without EDNS(0) must not carry OPT.
func (h *Handler) setEDNS(m *dns.Msg, r *dns.Msg) {
	if opt := r.IsEdns0(); opt != nil {
		m.SetEdns0(dns.DefaultMsgSize, opt.Do())
	}
}

// maxSize is the byte budget for the response: unlimited-ish over TCP, and over
// UDP the client's advertised EDNS(0) buffer, floored at the classic 512 bytes.
// Two TXT values at one name — the wildcard-plus-apex case — routinely exceeds
// 512, and TC is what makes the client come back over TCP.
func (h *Handler) maxSize(w dns.ResponseWriter, r *dns.Msg) int {
	if _, ok := w.RemoteAddr().(*net.TCPAddr); ok {
		return dns.MaxMsgSize
	}
	size := dns.MinMsgSize
	if opt := r.IsEdns0(); opt != nil {
		size = int(opt.UDPSize())
	}
	switch {
	case size < dns.MinMsgSize:
		return dns.MinMsgSize
	case size > dns.MaxMsgSize:
		return dns.MaxMsgSize
	}
	return size
}
