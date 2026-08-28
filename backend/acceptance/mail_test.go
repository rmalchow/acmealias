//go:build acceptance

package acceptance

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"
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

type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

// TestBothSendersShareOneIdentity provokes one message from EACH sender, reads
// both out of mailpit, and requires the same sender identity and the same footer
// (transactional-email T2, task 5.1).
//
// It used to compare the two CONFIGURATIONS instead — the service's effective
// config against AUTHENTIK_EMAIL__FROM in the compose file — because provoking
// an authentik message meant driving an enrolment flow that did not exist yet.
// `self-service-signup` built it, so this is the form the task actually asked
// for: what arrived, not what was configured. The difference is not pedantry.
// A configuration comparison cannot see a template that renders no footer, a
// display name the sending library rewrites, or a plain-text alternative that is
// silently empty because somebody added `X.html` and no `X.txt` — and every one
// of those is a way the two senders diverge while both configurations still
// agree.
//
// THE FOOTER IS THE HALF THAT WAS UNASSERTED FOR LONGER THAN ANYBODY THOUGHT.
// The service carries it as configuration and appends it beneath a `-- `
// separator. authentik carried none at all: it renders Django templates whose
// context is four values — url, user, expires, token — with no settings access,
// so it signed off with "Powered by goauthentik.io." where the sender's
// identification belongs. The fix is a mounted template directory
// (development/authentik/templates/) whose footer is a literal, exactly as the
// sender address is a literal in compose.yaml, and this test is what keeps the
// literal honest.
func TestBothSendersShareOneIdentity(t *testing.T) {
	idp := provokeIdentityProviderMessage(t)
	svc := provokeServiceMessage(t)

	if !strings.EqualFold(svc.From.Address, idp.From.Address) {
		t.Errorf("the two senders sent from different addresses:\n"+
			"  service:   %s\n  authentik: %s\n"+
			"Mail clients thread by sender, so these arrive as two correspondents and a customer "+
			"looking for one of our messages finds a conversation that does not contain it. In "+
			"production it also costs authentication alignment, which files the message as spam "+
			"rather than bouncing it.", svc.From.Address, idp.From.Address)
	}
	if svc.From.Name != idp.From.Name {
		t.Errorf("the two senders used different display names: service %q, authentik %q — "+
			"the name is what a recipient actually sees, and two of them read as two senders",
			svc.From.Name, idp.From.Name)
	}

	svcFooter, idpFooter := footerOf(svc.Text), footerOf(idp.Text)
	switch {
	case svcFooter == "":
		t.Errorf("the service's message carries no footer beneath a `-- ` separator. "+
			"mail.footer is validated as non-empty at start-up, so this is the RENDERING "+
			"having lost it:\n%s", svc.Text)
	case idpFooter == "":
		t.Errorf("authentik's message carries no footer beneath a `-- ` separator. Its email "+
			"stage must name a template under development/authentik/templates/, and that "+
			"directory must be mounted at /templates in the WORKER — the worker renders and "+
			"sends; a template mounted only in the server validates and then fails at send "+
			"time:\n%s", idp.Text)
	case svcFooter != idpFooter:
		t.Errorf("the two senders carry different footers:\n  service:   %q\n  authentik: %q\n"+
			"This is the §5 identification — who is sending and where they can be written to — "+
			"and two spellings of it means one of them is stale. The service reads it from "+
			"config.yaml (mail.footer, ACME_MAIL_FOOTER); authentik cannot, because an email "+
			"stage renders with a context of four values and no settings access, so its copy "+
			"is a literal in development/authentik/templates/acmealias/_footer.txt and must be "+
			"edited alongside", svcFooter, idpFooter)
	}
}

// provokeIdentityProviderMessage registers a stranger and returns the
// confirmation message authentik actually sent.
//
// It walks the real enrolment flow rather than calling an API, because the
// message only exists as the side effect of a flow reaching an email stage —
// which is also the only thing that proves the stage is BOUND, and pointed at
// the template this change installed.
func provokeIdentityProviderMessage(t *testing.T) mailpitFullMessage {
	t.Helper()
	username := fmt.Sprintf("mailagreement-%d", time.Now().UnixNano())
	address := username + "@corp.example"
	t.Cleanup(func() { deleteAuthentikUser(t, username) })

	client := &http.Client{Jar: newLaxJar(), Timeout: 30 * time.Second}
	step, err := enrolStep(client, "", map[string]string{
		"component":       "ak-stage-prompt",
		"username":        username,
		"name":            "A Stranger",
		"email":           address,
		"password":        "enrol-" + username,
		"password_repeat": "enrol-" + username,
	})
	if err != nil {
		t.Fatalf("submitting the enrolment form: %v", err)
	}
	if step.Component != "ak-stage-email" {
		t.Fatalf("the enrolment flow is at %q rather than its email stage, so no message "+
			"was sent and there is nothing to compare", step.Component)
	}
	return awaitMessage(t, address)
}

// provokeServiceMessage buys a team account and invites somebody, which is the
// only thing this service currently sends mail about.
//
// A team account is bought rather than reused because inviting is refused on a
// personal one — an account of one cannot have members — and buying it is three
// lines through the same endpoint the console's Upgrade screen uses.
func provokeServiceMessage(t *testing.T) mailpitFullMessage {
	t.Helper()
	owner := mustSignIn(t, adminUser, devPassword)
	slug := unique("acc-mail")
	var account accountView
	if err := owner.json(http.MethodPost, "/api/v1/accounts",
		map[string]any{"slug": slug, "name": "Acceptance (mail)", "amount": 25},
		http.StatusCreated, &account); err != nil {
		t.Fatalf("buying a team account to invite from: %v — the dev stack sets "+
			"ACME_ALLOW_ACCOUNT_CREATION=true, and without it there is no way to reach an "+
			"account that may invite", err)
	}
	owner.account = account.ID

	// A fresh address every run, because the per-address daily limit is real and
	// a suite that reused one would start failing on its second pass of the day
	// with a refusal that looks like a broken endpoint.
	address := fmt.Sprintf("invited-%d@corp.example", time.Now().UnixNano())
	if err := owner.json(http.MethodPost, owner.acct("/invitations"),
		map[string]any{"email": address}, http.StatusCreated, nil); err != nil {
		t.Fatalf("inviting %s: %v", address, err)
	}
	return awaitMessage(t, address)
}

type mailpitFullMessage struct {
	From    mailpitAddress `json:"From"`
	Subject string         `json:"Subject"`
	Text    string         `json:"Text"`
	HTML    string         `json:"HTML"`
}

// awaitMessage waits for the message sent to address and returns it whole.
//
// Polling rather than a fixed wait: both senders queue — authentik through its
// worker, the service after its transaction commits — and a sleep long enough
// to be reliable is long enough to be paid on every run.
func awaitMessage(t *testing.T, address string) mailpitFullMessage {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var page struct {
			Messages []struct {
				ID string           `json:"ID"`
				To []mailpitAddress `json:"To"`
			} `json:"messages"`
		}
		id := ""
		if err := getJSONFrom(mailpitAPI+"/messages?limit=100", &page); err == nil {
			for _, m := range page.Messages {
				for _, to := range m.To {
					if strings.EqualFold(to.Address, address) {
						id = m.ID
					}
				}
			}
		}
		if id != "" {
			var msg mailpitFullMessage
			if err := getJSONFrom(mailpitAPI+"/message/"+id, &msg); err != nil {
				t.Fatalf("reading the message to %s: %v", address, err)
			}
			return msg
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message reached %s within 30s. Both senders point at the compose "+
				"service name `mailpit:1025`; mail.localhost is the WEB interface and SMTP "+
				"does not traverse the HTTP overlay", address)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// footerOf returns what follows the last signature separator, which is where a
// footer lives in a plain-text message.
//
// The separator is `-- ` alone on a line, trailing space included — RFC 3676
// §4.3, and the thing mail clients look for when they decide what to hide behind
// "show trimmed content". Both senders write it: the service in
// internal/mail.(*SMTP).Render, authentik in the mounted .txt templates. The
// text part is read rather than the HTML because it is the one both produce
// without markup in the way, and because a missing .txt — the failure mode of
// adding a custom template and forgetting its plain-text half — shows up here as
// an empty body rather than as a passing test.
func footerOf(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	const sep = "\n-- \n"
	i := strings.LastIndex(body, sep)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(body[i+len(sep):])
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

// The two configuration readers that used to live here — one for the service's
// effective config, one grepping AUTHENTIK_EMAIL__FROM out of compose.yaml — are
// gone. Comparing what ARRIVED subsumes them: a sender that is configured
// correctly and rendered wrongly passed the old pair and fails this one.

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
