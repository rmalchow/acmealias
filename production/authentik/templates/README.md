# authentik's email templates

Mounted at `/templates`, which is `email.template_dir` in authentik's own
configuration and the second entry in Django's template `DIRS`. Anything ending
in `.html` under here appears in an email stage's template list as
`Custom Template: <path>`, and the blueprint names one by that relative path.

**A template needs BOTH halves.** `TemplateEmailMessage` renders the `.html` and
then renders the same path with `html` replaced by `txt` for the plain-text
alternative — silently skipping it if it does not exist, which leaves a message
whose text part is empty. Every mail client that prefers text, and every spam
filter that scores an HTML-only message, sees that.

**Why we have any of these at all.** authentik's built-in templates carry
"Powered by goauthentik.io." where a transactional message must carry the
sender's identification, and they call the thing being recovered "your authentik
account" — the name of software the recipient did not sign up for and does not
know we run. The footer is `_footer.txt` / `_footer.html`, included by both
templates so this side has one place to edit; the service carries the same
sentence as `mail.footer` in `config.yaml`. Nothing can share it between them —
an email stage renders with a context of four values and no settings access —
so the agreement is asserted rather than factored out, by
`TestBothSendersShareOneIdentity` in `backend/acceptance`.

The upgrade hazard is that these extend `email/base.html` from the image. A
version that renames that template or its `content` / `sub_content` blocks
breaks rendering, and the symptom is a configuration-error event in authentik
plus a person who never receives a confirmation link. The acceptance suite
reads a real message out of mailpit, so it fails rather than the deployment.
