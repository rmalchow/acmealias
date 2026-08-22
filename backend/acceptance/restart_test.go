//go:build acceptance

package acceptance

import (
	"strings"
	"testing"
	"time"
)

// Task 2.5: the durability claim (D6), and the reason challenges live in
// PostgreSQL rather than in memory.
//
// The api container is restarted in the window between the challenge being
// published and the CA validating it. If the value only existed in the
// process, the restart would take it with it and the order would fail.
//
// The second assertion — that the container really did restart — is not
// decoration. Without it, a restart that silently did nothing leaves a test
// that merely repeats task 2.1 and reports success forever.
func TestRestartBetweenPublishAndValidation(t *testing.T) {
	before := apiStartedAt(t)

	var after string
	iss, err := runIssuance(t, func() {
		// Called the moment the first challenge value is answerable, and
		// before the CA has looked.
		compose(t, "restart", "api")
		// The binary is rebuilt and restarted by air inside the container, so
		// wait for the listeners to come back rather than assuming.
		eventually(t, 3*time.Minute, "the api to answer again after its restart", func() error {
			_, _, err := txtValues(serviceDNS, aliasFQDN)
			return err
		})
		after = apiStartedAt(t)
	})
	if err != nil {
		t.Fatalf("issuance across an api restart: %v", err)
	}

	if after == "" || after == before {
		t.Fatalf("the api container did not restart (StartedAt %q both before and after); "+
			"without that this test is only a second copy of TestCertificateIsIssuedAndChainsToPebble", before)
	}
	if !issuedByPebble(demoDomain) {
		t.Fatalf("no pebble-issued certificate after the restart")
	}
	if iss.maxTXT == 0 {
		t.Fatalf("no challenge value was ever observed, so nothing was restarted mid-issuance")
	}
}

// apiStartedAt is the api container's start timestamp, as docker reports it.
func apiStartedAt(t *testing.T) string {
	t.Helper()
	id := strings.TrimSpace(compose(t, "ps", "-q", "api"))
	if id == "" {
		t.Fatal("the api container is not running")
	}
	out, err := run("docker", "inspect", "-f", "{{.State.StartedAt}}", id)
	if err != nil {
		t.Fatalf("docker inspect api: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}
