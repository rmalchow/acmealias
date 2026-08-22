// Package dnsserver is the authoritative DNS responder for the challenge zone
// (design D7). It answers TXT for alias labels, SOA and NS at the apex, and
// refuses everything else; it never recurses and never touches the database on
// the query path — every answer comes from the in-memory zoneview.
package dnsserver

import (
	"context"
	"log/slog"
	"net"

	"github.com/miekg/dns"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/config"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/obs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/queryobs"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/zoneview"
)

// Server runs one Handler on a UDP and a TCP listener at the same address. TCP
// is not optional: a truncated UDP answer is only useful if the retry lands.
type Server struct {
	addr string
	h    *Handler
	log  *slog.Logger

	udp, tcp *dns.Server
}

// queries may be nil, which disables query-observation counting; the handler
// treats a nil counter as a no-op.
func New(cfg *config.Config, view *zoneview.View, metrics *obs.Metrics, queries *queryobs.Counter, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{addr: cfg.DNSListenAddr, h: NewHandler(cfg, view, metrics, queries), log: log}
}

// Start binds both listeners and serves in the background. A bind failure is
// returned synchronously — the caller learns port 53 is taken before it reports
// itself ready — while serving errors after a successful bind are logged.
func (s *Server) Start() error {
	pc, err := net.ListenPacket("udp", s.addr)
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		pc.Close()
		return err
	}

	s.udp = &dns.Server{PacketConn: pc, Handler: s.h}
	s.tcp = &dns.Server{Listener: l, Handler: s.h}
	for _, srv := range []*dns.Server{s.udp, s.tcp} {
		go func(srv *dns.Server) {
			// ActivateAndServe returns nil on Shutdown; anything else is a
			// listener that died under us and is worth a loud line.
			if err := srv.ActivateAndServe(); err != nil {
				s.log.Error("dns listener stopped", "net", srv.Net, "addr", s.addr, "error", err)
			}
		}(srv)
	}
	return nil
}

// UDPAddr and TCPAddr report the bound addresses, which is how a caller that
// asked for port 0 finds out what it got.
func (s *Server) UDPAddr() net.Addr { return s.udp.PacketConn.LocalAddr() }
func (s *Server) TCPAddr() net.Addr { return s.tcp.Listener.Addr() }

// Shutdown stops both listeners, waiting for in-flight queries within ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	var first error
	for _, srv := range []*dns.Server{s.udp, s.tcp} {
		if srv == nil {
			continue
		}
		if err := srv.ShutdownContext(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
