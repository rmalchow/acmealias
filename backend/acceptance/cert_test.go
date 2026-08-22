//go:build acceptance

package acceptance

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// Task 2.1: a genuine issuance happened, and what is on the wire is the
// certificate the fake CA issued.
//
// Verifying is the whole point rather than a formality. Traefik's fallback when
// issuance fails is to serve its own self-signed "TRAEFIK DEFAULT CERT", and it
// serves it on the same port with the same SNI — so a test that fetched the
// page with verification off would be green against a stack where no
// certificate was ever issued and no challenge was ever published.
func TestCertificateIsIssuedAndChainsToPebble(t *testing.T) {
	forcedIssuance(t)

	pool, certs, err := pebbleChain()
	if err != nil {
		t.Fatalf("fetching pebble's chain: %v", err)
	}
	// Root AND intermediate. Pebble issues from the intermediate, so a pool
	// holding only /roots/0 does not verify a leaf.
	if len(certs) != 2 {
		t.Fatalf("pebble chain: got %d certificates, want 2 (a root and an intermediate)", len(certs))
	}

	// Fetch the site through traefik-lb's published :443, verifying against
	// that pool. --resolve, in effect: the fake example.com zone lives in the
	// stack's CoreDNS, which the host does not use.
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, lbAddr)
			},
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get("https://" + demoDomain + "/")
	if err != nil {
		t.Fatalf("GET https://%s/ through traefik-lb: %v", demoDomain, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)) //nolint:errcheck // draining
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET https://%s/: got %d, want 200", demoDomain, resp.StatusCode)
	}

	// And explicitly, so the failure names the certificate rather than the
	// transport: the served leaf verifies for this name against pebble's chain.
	chain, err := servedChain(demoDomain)
	if err != nil {
		t.Fatalf("reading the served certificate: %v", err)
	}
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}
	// Proof that the verification above is not vacuous, and a standing reminder
	// of why the pool needs two certificates: pebble signs leaves with its
	// INTERMEDIATE, so a pool holding only /roots/0 rejects this certificate.
	// Anyone who "fixes" trust.go down to the root alone fails here rather than
	// somewhere unrelated with an "unknown authority" message.
	rootOnly := x509.NewCertPool()
	rootOnly.AddCert(certs[0])
	if _, err := chain[0].Verify(x509.VerifyOptions{Roots: rootOnly, DNSName: demoDomain}); err == nil {
		t.Error("the leaf verified against pebble's root alone; the chain fetched from " +
			"the management API is then not what is being asserted")
	}

	if _, err := chain[0].Verify(x509.VerifyOptions{
		Roots: pool, Intermediates: intermediates, DNSName: demoDomain,
	}); err != nil {
		t.Fatalf("the served certificate does not verify against pebble's chain "+
			"(issuer %q) — if that issuer is TRAEFIK DEFAULT CERT, no certificate was issued at all: %v",
			chain[0].Issuer, err)
	}
}
