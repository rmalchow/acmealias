package certobserve

import (
	"os/exec"
	"strings"
	"testing"
)

// selfPath is this package, as go list names it.
const selfPath = "gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/certobserve"

// 4.6: nothing in this change may be reachable from a request path.
//
// /present is called by lego in the MIDDLE of an issuance. A TLS handshake
// against a customer's load balancer on that path would make certificate
// issuance depend on a third party's availability and on a five-second timeout,
// which is a far worse failure than any dot is worth — and it is a failure one
// import statement away, added by somebody who wants to "refresh the observation
// while we are here". This walks the real dependency graph and fails on it,
// which is cheaper than the incident.
//
// internal/dnsserver is the DNS answer path, which must not touch the database
// at all, let alone dial out. It is checked here because it is a package whose
// whole boundary is expressible as a dependency.
//
// THE CHALLENGE HANDLERS CANNOT BE CHECKED THIS WAY and that is worth stating
// rather than leaving as a gap: /present lives in internal/httpapi, one package
// with every other route in it, and the observation READ (task 5.2) is served
// from that same package — so httpapi imports certobserve legitimately and a
// dependency assertion about it would be a test that can only ever be deleted.
// What holds that half of the line is that this package exports Run, Pass and
// the banding, and nothing else: there is no exported "observe this domain now"
// for a handler to call.
func TestObservationIsNotReachableFromTheAnswerPath(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("skipping: no go toolchain on PATH to walk the dependency graph with")
	}
	const answerPath = "gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/dnsserver"

	out, err := exec.Command("go", "list", "-deps", answerPath).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", answerPath, err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == selfPath {
			t.Fatalf("%s depends on %s: the DNS answer path must not be able to reach a TLS handshake",
				answerPath, selfPath)
		}
	}
}
