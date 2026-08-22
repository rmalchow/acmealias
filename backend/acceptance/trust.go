//go:build acceptance

package acceptance

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// The fake CA's trust chain, fetched from pebble's management API.
//
// THREE certificates are in play here and conflating them is the classic trap:
//
//   - development/data/pebble/minica.pem — extracted from the pebble image by
//     up.sh — secures pebble's OWN endpoints. It is what lets a client reach
//     :14000 and :15000 at all (traefik-lb gets it as LEGO_CA_CERTIFICATES).
//     It signs nothing this service or Traefik ever serves.
//   - https://127.0.0.1:15000/roots/0 is the root pebble generates per run.
//   - https://127.0.0.1:15000/intermediates/0 is what it actually signs leaves
//     with.
//
// BOTH of the latter two are required. A pool holding only the root does not
// verify a leaf, and the failure reads as an unrelated "x509: certificate
// signed by unknown authority" that sends you looking in the wrong place.
// development/scripts/issue.sh concatenates both for `curl --cacert`; this is
// the Go equivalent.
//
// Nothing in this suite ever sets InsecureSkipVerify on the path under test.
// It matters more than it sounds: when issuance fails, Traefik falls back to
// serving a self-signed "TRAEFIK DEFAULT CERT" of its own, so a suite that
// skipped verification would pass green against a stack where no certificate
// was ever issued.

var (
	chainOnce  sync.Once
	chainPool  *x509.CertPool
	chainCerts []*x509.Certificate
	chainErr   error
)

// pebbleChain returns the issuing chain as a pool and as the certificates it
// was built from (the tests assert there are two). Fetched once per run.
func pebbleChain() (*x509.CertPool, []*x509.Certificate, error) {
	chainOnce.Do(func() {
		pool := x509.NewCertPool()
		for _, path := range []string{"/roots/0", "/intermediates/0"} {
			cert, err := fetchPEMCert(pebbleMgmt + path)
			if err != nil {
				chainErr = fmt.Errorf("pebble %s: %w", path, err)
				return
			}
			pool.AddCert(cert)
			chainCerts = append(chainCerts, cert)
		}
		chainPool = pool
	})
	return chainPool, chainCerts, chainErr
}

// fetchPEMCert reads one PEM certificate from pebble's management API.
//
// InsecureSkipVerify is deliberate and confined to this one call. Pebble's
// management endpoint presents a certificate issued by its own minica for the
// name "localhost", and we reach it on 127.0.0.1 — the same reason
// development/scripts/issue.sh uses `curl -sk` here. This is the endpoint we
// fetch the trust anchors FROM; it is not on the path any assertion verifies.
func fetchPEMCert(url string) (*x509.Certificate, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see doc comment
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no PEM certificate in the response")
	}
	return x509.ParseCertificate(block.Bytes)
}
