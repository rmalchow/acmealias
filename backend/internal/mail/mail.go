// Package mail is how this service sends transactional messages, and — more
// importantly — what it means for a message to be from us.
//
// # Two senders, one identity
//
// This deployment sends mail from two programs. The identity provider sends
// about signing in: verification, password recovery, and (once
// self-service-signup lands) the message telling somebody their registration is
// being reviewed. This service sends about the product: invitations, warnings
// that an account is about to stop working, notices that a delegation never
// appeared.
//
// They cannot share a line of code. authentik renders Django templates in
// Python; this renders Go templates in Go. So what they share is a
// SPECIFICATION — the sender identity, the reply address and the footer, all of
// them configuration (config.Config.Mail*) that both are pointed at — and the
// only thing that keeps two implementations honest about a specification is a
// test that reads what each actually put on the wire. The development stack
// routes both to mailpit, whose API a test can read; that test is
// transactional-email task 5.1.
//
// # The part that fails silently
//
// The sender is not presentation.
//
// Mail clients thread by sender, so two spellings of us — `acmealias` and
// `AcmeAlias`, `no-reply@` and `noreply@` — arrive as two correspondents, and a
// customer looking for "that invitation" finds a conversation that does not
// contain it.
//
// Worse, every host that sends on the domain's behalf must be authorised by the
// domain's published sender policy AND sign with a published key, and the result
// must ALIGN with the address a reader sees. A message can pass SPF for the host
// that sent it and still be filed as spam because the domain that passed is not
// the domain in the visible From. NONE OF THAT BOUNCES. There is no error to
// notice: the message is accepted and filed away from the inbox, and the first
// report is a customer saying they never received something we can see we sent.
//
// The DNS side of that is deployment work (transactional-email task group 2),
// not code, and this package cannot check it. What it can do is make the
// identity one value rather than a string repeated in each template, which is
// the half that is ours to get wrong.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/smtp"
	"strings"
)

// Identity is who the deployment is when it sends. Both senders are configured
// from the same values; see the package comment for why that is load-bearing.
type Identity struct {
	// From is the address messages are sent as, and Name is the display name
	// beside it.
	From string
	Name string

	// ReplyTo goes somewhere a person reads.
	//
	// `no-reply@` is the reflex and it is wrong for everything this service
	// sends (transactional-email T3). An invitation, a warning that an account
	// is about to stop working, a notice that a delegation never appeared —
	// every one is something a recipient may reasonably want to answer, and an
	// address that discards mail teaches them that answering us does not work.
	// They remember that later, when we need them to answer.
	//
	// From may remain a no-reply sender. This may not be one.
	ReplyTo string

	// Footer identifies the sender, and it is not decoration: a transactional
	// message says who sent it, and the jurisdiction the company operates in
	// requires particular details in it. Settled here rather than per-message so
	// that the fifth template cannot quietly disagree with the first four (T5).
	Footer string
}

// Validate reports what is missing or malformed, all of it at once.
//
// It returns every problem rather than the first, because these are read at
// start-up by somebody fixing a configuration file: reporting one error per
// restart turns a single edit into four.
func (id Identity) Validate() error {
	var problems []string
	if id.From == "" {
		problems = append(problems, "mail.from is empty")
	} else if _, err := mail.ParseAddress(id.From); err != nil {
		problems = append(problems, fmt.Sprintf("mail.from %q is not an address: %v", id.From, err))
	}
	if id.Name == "" {
		problems = append(problems, "mail.sender is empty; it is the name a recipient sees, "+
			"and mail clients thread by it")
	}
	// An empty ReplyTo is a refusal to be answered, and T3 argues that is a
	// decision rather than a default. It is reported rather than substituted,
	// because substituting From here is exactly the no-reply outcome the
	// decision rejects, done silently.
	if id.ReplyTo == "" {
		problems = append(problems, "mail.reply_to is empty; every message this service sends is "+
			"something a recipient may reasonably want to answer, and an address that discards "+
			"replies teaches them that answering us does not work")
	} else if _, err := mail.ParseAddress(id.ReplyTo); err != nil {
		problems = append(problems, fmt.Sprintf("mail.reply_to %q is not an address: %v", id.ReplyTo, err))
	}
	if id.Footer == "" {
		problems = append(problems, "mail.footer is empty; a transactional message says who sent it")
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// Message is one message to one recipient.
//
// Subject follows the grammar of transactional-email T4: the thing the message
// concerns comes FIRST, and there is no constant prefix identifying us.
//
//	Invitation to cloudpowered              not  [acmealias] You have been invited
//	Credit low: cloudpowered                not  [acmealias] Warning about your credits
//	Delegation not published: shop.example  not  [acmealias] Action required
//
// A prefix spends the most valuable characters in an inbox — the ones visible in
// a notification and in a narrow list — on what the sender column already shows.
// It is a habit inherited from mailing lists, where the sender genuinely was
// ambiguous. "Action required" is what a subject says when nobody decided what
// it was about.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a message. It is an interface so that a test can assert what
// was produced without an SMTP server, and so that a deployment with no mail
// configured gets Discard rather than a nil dereference.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends over SMTP.
//
// There is deliberately no template rendering here and no message bodies: the
// messages belong to the changes that send them (`account-invitations` the
// invitation, `metering-and-credits` the credit warning). This is the shape they
// share (transactional-email task 3.1).
type SMTP struct {
	Identity Identity

	// Addr is host:port.
	//
	// In development this is `mailpit:1025` — the compose SERVICE NAME. It is
	// not `mail.localhost`, which is mailpit's web interface and is served
	// through the HTTP overlay; SMTP does not traverse that, and a deployment
	// that sets the web name here fails at the first message with a protocol
	// error that reads like a broken mail server.
	Addr string

	// Auth is nil when the relay wants none, which is the development case.
	Auth smtp.Auth
}

// Send writes one message.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Addr == "" {
		return errors.New("mail: no SMTP address configured")
	}
	if _, err := mail.ParseAddress(m.To); err != nil {
		return fmt.Errorf("mail: recipient %q is not an address: %w", m.To, err)
	}
	from, err := mail.ParseAddress(s.Identity.From)
	if err != nil {
		return fmt.Errorf("mail: sender %q is not an address: %w", s.Identity.From, err)
	}
	return smtp.SendMail(s.Addr, s.Auth, from.Address, []string{m.To}, s.Render(m))
}

// Render builds the wire form: headers, the body, and the footer beneath it.
//
// It is exported because it is what the agreement between the two senders is
// asserted against — a test can compare what this produces with what the
// identity provider produced, without either of them having sent anything.
func (s *SMTP) Render(m Message) []byte {
	id := s.Identity
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", (&mail.Address{Name: id.Name, Address: addressOf(id.From)}).String())
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	if id.ReplyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", id.ReplyTo)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(m.Body, "\n", "\r\n"))
	if id.Footer != "" {
		b.WriteString("\r\n\r\n-- \r\n")
		b.WriteString(strings.ReplaceAll(id.Footer, "\n", "\r\n"))
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// addressOf strips a display name that may already be present in the configured
// From, so that a deployment writing `AcmeAlias <no-reply@example.com>` there
// gets one display name rather than two.
func addressOf(s string) string {
	if a, err := mail.ParseAddress(s); err == nil {
		return a.Address
	}
	return s
}

// Discard accepts messages and drops them. It is what a deployment with no mail
// configured gets, so that an unconfigured deployment fails to SEND rather than
// failing to serve — a missing invitation is bad and a panic in the request that
// issues it is worse.
type Discard struct{}

func (Discard) Send(context.Context, Message) error { return nil }
