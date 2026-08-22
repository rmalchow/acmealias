//go:build acceptance

package acceptance

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type challengeRow struct {
	AliasLabel string `json:"aliasLabel"`
	Value      string `json:"value"`
}

// Task 2.3: both ends of the challenge's life fired, and nothing is left over.
//
// The third assertion is the one a naive implementation gets wrong (D7). After
// cleanup the alias name must answer NOERROR with an empty answer section — it
// still EXISTS, it just holds no values right now. NXDOMAIN would say the name
// does not exist at all, which is a different and wrong statement: it tells a
// resolver it may cache the non-existence and it makes the next renewal look
// like a misconfigured delegation.
func TestPresentAndCleanupBothFiredAndNothingRemains(t *testing.T) {
	iss := forcedIssuance(t)

	logs := compose(t, "logs", "--since", iss.start.Format(time.RFC3339), "api")
	for _, event := range []string{"challenge_publish", "challenge_withdraw"} {
		if !strings.Contains(logs, event) {
			t.Errorf("no %s in the api log for this issuance — "+
				"/present and /cleanup must BOTH be called", event)
		}
	}

	s := mustSignIn(t, adminUser, devPassword)

	// /cleanup lands a moment after the certificate is on the wire, so this is
	// a wait rather than an instant read.
	eventually(t, 2*time.Minute, "challenge rows for "+aliasLabel+" to disappear", func() error {
		var rows []challengeRow
		if err := s.getJSON("/api/v1/challenges", &rows); err != nil {
			return err
		}
		for _, r := range rows {
			if r.AliasLabel == aliasLabel {
				return fmt.Errorf("still holding %d row(s), e.g. %q", len(rows), r.Value)
			}
		}
		return nil
	})

	r, err := query(serviceDNS, aliasFQDN, dns.TypeTXT)
	if err != nil {
		t.Fatalf("querying %s: %v", aliasFQDN, err)
	}
	if r.Rcode != dns.RcodeSuccess {
		t.Fatalf("%s answered %s after a completed issuance; it must be NOERROR with an "+
			"empty answer — the name exists, it just holds no values (D7)",
			aliasFQDN, dns.RcodeToString[r.Rcode])
	}
	if n := len(r.Answer); n != 0 {
		t.Fatalf("%s still answers %d record(s) after cleanup", aliasFQDN, n)
	}
}
