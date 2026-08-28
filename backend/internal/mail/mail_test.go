package mail

import (
	"strings"
	"testing"
)

func testIdentity() Identity {
	return Identity{
		From:    "no-reply@acmealias.com",
		Name:    "AcmeAlias",
		ReplyTo: "support@acmealias.com",
		Footer:  "AcmeAlias\nsupport@acmealias.com",
	}
}

// The rendering carries the identity, because the identity is the part that
// fails silently: a reader threads by it, and a receiver authenticates against
// it (transactional-email T1).
func TestRenderCarriesTheSenderIdentity(t *testing.T) {
	s := &SMTP{Identity: testIdentity()}
	got := string(s.Render(Message{To: "dana@corp.example", Subject: "Invitation to cloudpowered", Body: "hello"}))

	for _, want := range []string{
		`From: "AcmeAlias" <no-reply@acmealias.com>`,
		"Reply-To: support@acmealias.com",
		"Subject: Invitation to cloudpowered",
		"To: dana@corp.example",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered message is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "\r\n-- \r\nAcmeAlias") {
		t.Errorf("the footer is missing or not delimited:\n%s", got)
	}
}

// A From that already carries a display name must not produce two of them. This
// is the shape an operator most plausibly writes into the setting, since it is
// the shape they see in their own mail client.
func TestRenderDoesNotDoubleTheDisplayName(t *testing.T) {
	id := testIdentity()
	id.From = `AcmeAlias <no-reply@acmealias.com>`
	got := string((&SMTP{Identity: id}).Render(Message{To: "a@b.example", Subject: "x"}))

	from := ""
	for _, line := range strings.Split(got, "\r\n") {
		if strings.HasPrefix(line, "From: ") {
			from = line
		}
	}
	if strings.Count(from, "AcmeAlias") != 1 {
		t.Errorf("From names the sender %d times, want once: %q", strings.Count(from, "AcmeAlias"), from)
	}
}

// Every problem at once. These are read by somebody fixing a configuration file
// at start-up, and one error per restart turns a single edit into four.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	err := Identity{}.Validate()
	if err == nil {
		t.Fatal("an empty identity validated")
	}
	for _, want := range []string{"mail.from", "mail.sender", "mail.reply_to", "mail.footer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, which does not mention %s", err, want)
		}
	}
}

// An absent reply address is reported rather than filled in from From.
// Substituting it would produce exactly the no-reply outcome T3 rejects, and
// would do it silently — which is worse than the mistake it papers over.
func TestValidateRefusesToSubstituteAReplyAddress(t *testing.T) {
	id := testIdentity()
	id.ReplyTo = ""
	err := id.Validate()
	if err == nil {
		t.Fatal("an identity with no reply address validated; T3 makes that a decision, not a default")
	}
	if !strings.Contains(err.Error(), "reply") {
		t.Errorf("Validate() = %q, which does not name the reply address", err)
	}
}

func TestValidateAcceptsACompleteIdentity(t *testing.T) {
	if err := testIdentity().Validate(); err != nil {
		t.Errorf("a complete identity did not validate: %v", err)
	}
}

// The subject grammar is a rule about content rather than about code, so what is
// asserted here is the one thing code can assert: the subject is passed through
// untouched, with no prefix bolted onto it (T4).
func TestSubjectIsNotPrefixed(t *testing.T) {
	const subject = "Delegation not published: shop.example.com"
	got := string((&SMTP{Identity: testIdentity()}).Render(Message{To: "a@b.example", Subject: subject}))
	if !strings.Contains(got, "Subject: "+subject+"\r\n") {
		t.Errorf("the subject was not passed through verbatim:\n%s", got)
	}
}
