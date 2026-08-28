package certobserve

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// serveTLS starts a listener on loopback that presents a self-signed
// certificate with the given not-after and names, and returns its "host:port".
//
// One helper, parameterised, because every interesting case in this package is
// the same handshake against a different certificate: expired, not covering the
// name, healthy, on a band edge. Writing a listener per case would make each of
// them look like a different kind of test when the only thing that varies is the
// certificate.
//
// It is self-signed and therefore never trusted by the system pool, which is
// exactly what makes it useful: a verifying client would refuse most of these,
// and the whole point of the package is that we do not (G12).
func serveTLS(t *testing.T, notAfter time.Time, names ...string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(0xC0FFEE),
		Subject:      pkix.Name{CommonName: "certobserve test"},
		// NotBefore is well in the past so that "expired" is the only thing an
		// expired case is testing: a certificate that is both not-yet-valid and
		// expired would band the same way for the wrong reason.
		NotBefore:   time.Now().Add(-365 * 24 * time.Hour),
		NotAfter:    notAfter,
		DNSNames:    names,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // the listener was closed by the cleanup above
			}
			// The handshake has to be driven before the connection is dropped:
			// tls.Listen's Accept returns before any bytes are exchanged, so
			// closing here would hand the observer an EOF and look like a
			// rejected handshake rather than a certificate. The observer sends
			// nothing afterwards, so there is nothing else to serve.
			go func() {
				_ = conn.(*tls.Conn).Handshake()
				conn.Close()
			}()
		}
	}()
	return ln.Addr().String()
}

// deadPort returns an address nothing is listening on, by binding one and
// letting it go. It is how the refused case is produced without depending on
// which ports happen to be free on the machine running the tests.
func deadPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// silentPort returns an address that accepts a connection and then says nothing
// at all — the blackhole case. It is the reason one deadline has to cover the
// dial AND the handshake: the dial here succeeds.
func silentPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		var held []net.Conn
		defer func() {
			for _, c := range held {
				c.Close()
			}
		}()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Held open, deliberately unanswered, until the listener closes.
			held = append(held, conn)
		}
	}()
	return ln.Addr().String()
}

// mustBool dereferences one of the observation's nullable judgements, failing
// the test rather than panicking when it is absent — an absent judgement on an
// observed certificate is itself a bug worth a legible message.
func mustBool(t *testing.T, what string, p *bool) bool {
	t.Helper()
	if p == nil {
		t.Fatalf("%s is nil on an observed certificate", what)
	}
	return *p
}

// dueFixture is the identity half of an observation — which domain, which node,
// which address — for the tests that are about the certificate half. The values
// are arbitrary and only have to be non-empty, because what they exercise is
// that the row explains itself without a join.
func dueFixture() store.DueObservation {
	return store.DueObservation{
		DomainID:   model.NewID(),
		AccountID:  model.NewID(),
		ProjectID:  model.NewID(),
		NodeID:     model.NewID(),
		ServerName: "shop.example.com",
		Family:     "A",
		Address:    "192.0.2.10",
	}
}
