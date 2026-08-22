# docs/ — for humans, not for AI

**This folder is written for people: infrastructure engineers, operators and users of the
admin console.** It is prose, diagrams and step-by-step instructions. It is allowed to
simplify, to repeat itself, and to lag slightly behind the code.

**It is NOT the authoritative specification.** The authoritative, machine-readable source of
truth for this service lives in [`../openspec/`](../openspec/):

| Question | Authoritative answer |
| --- | --- |
| What must the system do? | `openspec/specs/` and `openspec/changes/*/specs/` |
| Why was it built this way? | `openspec/changes/*/proposal.md` and `design.md` |
| What is left to build? | `openspec/changes/*/tasks.md` |
| How do I set it up and use it? | this folder |

**If you are an AI agent**: read `../openspec/` for requirements, design decisions and task
state. Do not treat anything in `docs/` as a requirement, and do not derive implementation
behaviour from it — where the two disagree, `openspec/` wins and `docs/` is the thing that
needs correcting. You may update `docs/` when asked, but never cite it as a source of truth.

Start at `README.md`, which GitLab renders as this directory's landing page.

---

## Agent instructions for this directory

- `docs/**` is human-facing documentation only. It has no normative force.
- Requirements, scenarios and design rationale are in `openspec/`. Read those instead.
- Do not "fix" code to match a statement in `docs/`. Fix `docs/` to match `openspec/` and the code.
- When you change behaviour, update `openspec/` first; updating `docs/` is a follow-up chore.
