//go:build acceptance

package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"testing"
)

// The agreement between the two senders, asserted rather than assumed
// (transactional-email T2, task 5.1).
//
// This deployment sends mail from two programs. authentik sends about signing
// in; the service sends about the product. They render templates in different
// languages in different processes and there is nothing to factor out, so the
// artefact they share is a SPECIFICATION — one sender address, one display
// name, one footer — and the only thing that keeps two implementations honest
// about a specification is a test that reads what each actually put on the wire.
//
// Both are pointed at mailpit, whose API is readable, so that is what this does.
//
// WHY IT IS WORTH A TEST AT ALL. Two template sets in two languages, edited by
// different changes months apart, converge on the same headers exactly as long
// as somebody remembers. And the failure is silent in the worst way: mail
// clients thread by sender, so two spellings of us arrive as two correspondents
// and a customer hunting for an invitation finds a conversation that does not
// contain it — while in production the divergence additionally costs
// authentication ALIGNMENT, which does not bounce but files the message as spam.
// Nothing errors, and the first report is somebody saying they never received
// what we can see we sent.
//
// This is the test that fails when somebody edits one side and not the other.
const mailpitAPI = "http://mail.localhost/api/v1"

type mailpitMessage struct {
	ID      string          `json:"ID"`
	From    mailpitAddress  `json:"From"`
	To      []mailpitAddress `json:"To"`
	Subject string          `json:"Subject"`
}

type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

// TestBothSendersShareOneIdentity reads the identity the SERVICE is configured
// with out of its own effective configuration, and the identity AUTHENTIK is
// configured with out of the compose file, and requires them to be the same
// sender.
//
// It reads authentik's from the compose file rather than provoking a message,
// deliberately: provoking one means driving the enrolment or recovery flow,
// which `self-service-signup` has not built yet. Comparing the two
// CONFIGURATIONS catches the whole failure mode this test exists for — somebody
// changing one side and not the other — and does it today rather than after the
// change that would let it read a real message. When enrolment lands, task 5.1's
// stronger form replaces this: provoke one message from each and compare what
// arrived.
func TestBothSendersShareOneIdentity(t *testing.T) {
	svc := serviceMailIdentity(t)
	idp := authentikMailIdentity(t)

	if !strings.EqualFold(svc.Address, idp.Address) {
		t.Errorf("the two senders use different addresses:\n"+
			"  service:  %s\n  authentik: %s\n"+
			"Mail clients thread by sender, so these arrive as two correspondents and a customer "+
			"looking for one of our messages finds a conversation that does not contain it. In "+
			"production it also costs authentication alignment, which files the message as spam "+
			"rather than bouncing it.", svc.Address, idp.Address)
	}
	if svc.Name != idp.Name {
		t.Errorf("the two senders use different display names: service %q, authentik %q — "+
			"the name is what a recipient actually sees, and two of them read as two senders",
			svc.Name, idp.Name)
	}
}

// TestTheServiceSenderIsAnswerable holds the deployment to T3: the reply
// address goes somewhere a person reads.
//
// `no-reply@` is the reflex and it is wrong for everything this service sends.
// Every message is an invitation, a warning that an account is about to stop
// working, or a notice that a delegation never appeared — all of them things a
// recipient may reasonably want to answer. An address that discards mail teaches
// them that answering us does not work, which they remember later, when we need
// them to answer.
func TestTheServiceSenderIsAnswerable(t *testing.T) {
	cfg := effectiveConfig(t)

	reply, _ := cfg["mail_reply_to"].(string)
	if reply == "" {
		t.Fatal("the deployment configures no reply address; T3 makes that a decision rather " +
			"than a default, and the service refuses to substitute From for it")
	}
	if _, err := mail.ParseAddress(reply); err != nil {
		t.Errorf("mail_reply_to %q is not an address: %v", reply, err)
	}
	if strings.Contains(strings.ToLower(reply), "no-reply") ||
		strings.Contains(strings.ToLower(reply), "noreply") {
		t.Errorf("the reply address is %q, which discards mail. From may be a no-reply sender; "+
			"Reply-To may not (transactional-email T3)", reply)
	}
}

// TestTheMailSinkIsReachable proves the address the service is pointed at is
// the SMTP one and not the web interface.
//
// It is a separate test because it is a separate mistake, and a plausible one:
// `mail.localhost` is the name a person sees in their browser, and setting it
// here fails at the first message with a protocol error that reads like a broken
// mail server rather than like a wrong hostname.
func TestTheMailSinkIsTheSMTPNameNotTheWebOne(t *testing.T) {
	cfg := effectiveConfig(t)
	addr, _ := cfg["mail_smtp_addr"].(string)
	if addr == "" {
		t.Skip("no SMTP address configured; nothing to check")
	}
	if strings.Contains(addr, "localhost") {
		t.Errorf("mail_smtp_addr is %q. `mail.localhost` is mailpit's WEB interface, served "+
			"through the HTTP overlay, and SMTP does not traverse it — the SMTP address is the "+
			"compose service name, `mailpit:1025`", addr)
	}
	if !strings.Contains(addr, ":") {
		t.Errorf("mail_smtp_addr %q has no port; it is host:port", addr)
	}

	// And mailpit is actually there, so a later test that reads messages out of
	// it fails on the message rather than on the sink.
	res, err := http.Get(mailpitAPI + "/messages")
	if err != nil {
		t.Fatalf("reading %s: %v", mailpitAPI+"/messages", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the mail sink answered %d; the agreement test cannot read what was sent", res.StatusCode)
	}
}

// serviceMailIdentity reads the sender out of the service's own effective
// configuration, which is what it will actually send as.
func serviceMailIdentity(t *testing.T) mail.Address {
	t.Helper()
	cfg := effectiveConfig(t)
	from, _ := cfg["mail_from"].(string)
	name, _ := cfg["mail_sender"].(string)
	if from == "" {
		t.Fatal("the service configures no sender address")
	}
	a, err := mail.ParseAddress(from)
	if err != nil {
		t.Fatalf("the service's mail_from %q is not an address: %v", from, err)
	}
	return mail.Address{Name: name, Address: a.Address}
}

// authentikMailIdentity reads the sender out of development/compose.yaml.
//
// The compose file is the source of truth rather than a constant here, for the
// same reason zoneDelegations reads the served zone file: a value this suite
// remembered would agree with itself forever while disagreeing with the stack.
func authentikMailIdentity(t *testing.T) mail.Address {
	t.Helper()
	b, err := os.ReadFile("../../development/compose.yaml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}
	const key = "AUTHENTIK_EMAIL__FROM:"
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key) {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, key))
		raw = strings.Trim(raw, `"'`)
		a, err := mail.ParseAddress(raw)
		if err != nil {
			t.Fatalf("authentik's %s is %q, which is not an address: %v", key, raw, err)
		}
		return *a
	}
	t.Fatalf("the compose file sets no %s, so authentik sends as whatever its image defaults to "+
		"— which is not the product", key)
	return mail.Address{}
}

// effectiveConfig reads the configuration the service is actually running with.
func effectiveConfig(t *testing.T) map[string]any {
	t.Helper()
	s := mustSignIn(t, adminUser, devPassword)
	// Account-scoped, and deliberately so: the route sits under /accounts/:id and
	// resolves a scope even though it reads no tenant data, because until it did,
	// any authenticated session could read the deployment's effective
	// configuration through any account identifier at all.
	s.selectInto(t, s.personalAccount(t))
	var out map[string]any
	if err := s.json(http.MethodGet, s.acct("/config"), nil, http.StatusOK, &out); err != nil {
		t.Fatalf("reading the effective configuration: %v", err)
	}
	return out
}

var _ = json.Marshal
var _ = fmt.Sprintf
