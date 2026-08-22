## Context

This is a vocabulary change, so most of the usual design questions do not arise. The two that do
are worth writing down, because both produced bugs while the rename was being applied and both
would produce them again for anyone repeating it.

## Goals / Non-Goals

**Goals**

- One word for the concept, everywhere: code, schema, routes, CLI, console, specs, human docs.
- No behavioural change, provable by the existing test suite passing unchanged in substance.

**Non-Goals**

- Renaming nodes, configuration variables, or the acceptance rig's `traefik-lb` container.
- Introducing a compatibility alias for the old routes. Nothing consumes them.

## Decisions

### R1. The members keep the name "node", and that is what makes the rename work

The obvious objection to "load balancer" is recursion: a load balancer made of load balancers.
It does not arise, because the members were already called nodes and stay called nodes. The
sentence the console renders is "the load balancer `prod` has two nodes, `a` and `b`" — a
sentence with no arity in it, which was the entire point.

**Rejected: "load balancer" for the member and "load balancer group" for the collection.** That
puts the counting word back and makes the common case ("I have one") read as a group of one.

### R2. Migration 000001 is edited in place, not followed by a rename migration

`000001_initial.up.sql` creates `lb_pairs`. The orthodox move is to leave it and add
`000007_rename_lb_pairs.up.sql` with `ALTER TABLE ... RENAME`.

Edited in place instead, because the orthodoxy protects something that does not exist here: a
deployment that already ran `000001`. There is none outside development stacks that are reset
from empty by `./scripts/dev.sh reset`. A rename migration would leave the initial schema
permanently describing a table nobody has, so every future reader of `000001` would have to know
to also read `000007` to learn the real name.

**This decision expires the moment the schema is applied anywhere that cannot be dropped.** After
that, a rename is a migration, and this note is the reason it will be.

### R3. A mechanical rename needs to know which "pair" means what

`pair` is an ordinary English word, and the codebase uses it in three senses that a blind
find-and-replace destroys:

| Sense | Example | Treatment |
| --- | --- | --- |
| The entity | "the pair's node addresses" | renamed |
| A tuple | `// Only the (alias, value) pair, never every value at the name` | protected |
| Two of something | "an identifier/secret pair", "`challenge_publish` / `challenge_withdraw` pairs" | protected |

The rename was applied as ordered passes — longest form first, so `LBPairID` was consumed by the
`LBPair` rule rather than half-rewritten — with the tuple senses substituted out beforehand and
restored afterwards.

Two failures are worth recording because neither was caught by the compiler:

- **An Angular template is not prose.** The rule `"of pairs" → "of load balancers"` rewrote a
  control-flow expression, `@for (pair of pairs(); track pair.id)`, into
  `@for (lb of load balancers(); ...)` — a template parse error rather than a type error, so it
  surfaced only when the dev server rebuilt.
- **Identifier rules leak across languages.** A rule written for Go locals (`"pair." → "lb."`)
  matched TypeScript too, leaving `this.lb.set(p)` in a component whose field is
  `loadBalancer`. The Angular compiler caught it; a less strict language would not have.

The lesson for anyone repeating this: run the build for **every** language in the repository
after each pass, and treat template and query languages as separate targets rather than as text.

### R4. The specs are renamed in place, and this change records why

`openspec/` holds the authoritative requirement text, and the vocabulary appears in ~96 places
across the unarchived changes. Renaming it in place — rather than adding a change that says "read
'pair' as 'load balancer' from now on" — keeps the corpus internally consistent, which is the
property that makes the specs usable at all.

What this change carries is the *decision*: the reasoning, the alternatives rejected, and the one
requirement whose meaning actually changed. A reader who wonders why the noun is what it is finds
the answer here, and the specs read as though it had always been so.

### R5. The documentation file is renamed too: `lb-setup.html` → `load-balancer-setup.html`

`docs/index.html` calls the third guide "3 · Per-load-balancer setup" while the file behind it is
still `lb-setup.html`, so the sidebar of all six pages links a name that no longer appears in the
text. The reflex objection is that a documentation URL is a promise to whoever bookmarked it, and
that objection is right in general.

It does not apply here, for two checkable reasons:

- **Nobody can hold that bookmark.** The repository is one commit (`a5e870c Initial commit`), and
  `.gitlab-ci.yml` has no `pages` job — `docs/` is not published anywhere. It is read by opening
  `docs/index.html` from a checkout, which is what `docs/README.md` tells a reader to do. A URL
  that has never been served is not a promise.
- **`docs/` is explicitly the side that gets corrected.** `docs/CLAUDE.md`: "where the two
  disagree, `openspec/` wins and `docs/` is the thing that needs correcting."

The same reasoning as R2, one directory over: the orthodoxy that says "keep the old path" protects
a reader who does not exist yet, at the cost of a filename that contradicts its own heading for
everyone who does. Both decisions expire on the same event — the first publication anyone outside
this repository can link to. After that, a docs rename needs a redirect, and this note is the
reason it will.

**Rejected: keep `lb-setup.html` and stop worrying about it.** It is cheaper by fifteen link
edits, and it leaves `lb` — the abbreviation of the abbreviation of the noun this change removed —
as the one place a reader still meets the old vocabulary, in the address bar, on the page that
exists to teach them the new one.

## Risks / Trade-offs

- **A vocabulary change loses its own history.** After this, a search for "LB pair" finds
  nothing, including in discussions and tickets written before it. Anyone reading an old thread
  has only this change to connect the two words — which is the argument for it existing.
- **`load_balancers` is longer than `lb_pairs`** in every query anyone writes by hand. Accepted:
  it is read far more often than typed, and `lb` survives as the local-variable convention.
- **The Go acceptance harness does not exist yet, so it cannot have been run.** An earlier draft
  of this design listed "the acceptance harness has not been run" as the outstanding risk.
  `backend/acceptance/` is not a directory — `scripts/test-acceptance.sh` opens with
  `[ -d backend/acceptance ] || die "backend/acceptance does not exist yet (task group 23)"`, and
  task group 23 of `add-acme-alias-service` is unchecked. There is nothing here to run against the
  renamed names, and nothing to carry forward: whoever writes those tests writes them against
  `--loadbalancer` because that is the only flag that will exist.

  What *does* exercise the flag end to end is the development stack.
  `development/scripts/seed.sh` drives the real CLI — `acmectl adminctl loadbalancer create`,
  `node add --loadbalancer demo/prod`, `domain create --loadbalancer …`, `key create
  --loadbalancer …`, `loadbalancer dns demo/prod` — and `./scripts/dev.sh issue` then takes the
  seeded fixture through a real DNS-01 issuance against Pebble. That is the check task 2.3 asks
  for, and it is runnable today by anyone with Docker.

- **The first mechanical pass was not complete, and "the build is green" did not reveal it.**
  Every occurrence still outstanding is in a name or a string the compiler has no opinion about:
  function names (`ListDomainsForPair`, `ListAliasesForPair`, `ListAPIKeysForPair`,
  `ListInteractionsForPair`, `ProjectIDForPair`), struct fields and their JSON tags
  (`PairSlug`/`PairName`, serialized as `pairSlug`/`pairName` and read back by
  `frontend/src/app/features/models.ts`), console methods (`createPair`, `deletePair`), test
  names (`TestIntegrationCrossPairAliasConflictIsRefused`), and prose in comments and specs.

  A separate residue is grammatical rather than lexical: substituting "load balancer" for a noun
  that began with a consonant left **`an load balancer`** in twenty-odd places, including two
  rendered documentation pages. Neither `go vet` nor `ng lint` reads English. The remaining task
  list turns both into `grep` invocations with a defined empty result, because that is the only
  form of this check a machine can fail.

## Migration Plan

There is none for data: the schema is created under the new names.

For a reviewer, the diff is worth reading in this order — schema first, then routes, then prose:

1. `backend/internal/store/migrations/000001_initial.up.sql` and `schema.sql` — the table and
   column rename, and nothing else.
2. `backend/internal/httpapi/admin.go` — the route table, which is the API's whole surface.
3. Everything else, which is the same two words repeated.

## Open Questions

- **Does `LBNode` want to become `Node`?** It is unambiguous inside `model`, and `Node` alone is
  not. Left as-is; raise it if the abbreviation reads oddly once the surrounding noun is spelled
  out.
- **Is `lb` the right local-variable convention in Go?** It is short, conventional and cannot be
  confused with anything else in scope, but it re-introduces the abbreviation the type name
  deliberately spells out.
