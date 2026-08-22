## Why

### A name that counts, applied to a thing that does not

The inventory's middle level has been called a **load balancer pair** since the first design. A
project owns them; each one owns nodes with addresses, the domains those nodes serve, the aliases
those domains delegate to, and the API keys allowed to answer challenges for them. It is the unit
the DNS plan is generated for and the unit an API key belongs to.

"Pair" was accurate about the fleet as it existed on the day it was named: two Traefik machines,
`a` and `b`, in front of each set of sites. It is not accurate about the concept. Nothing in the
schema, the plan generator or the challenge API cares how many nodes there are — the DNS plan
emits an A record per node address, and one node or five produce a correct plan. The word made a
promise the model never kept, and every reader who noticed had to be told "it can be more than
two, the name is historical".

That cost is small in isolation and compounds in three places:

- **The specs.** Requirement text is normative; a noun that implies an arity the requirements do
  not enforce is a contradiction a careful reader is entitled to trust and be misled by.
- **The human docs.** `docs/lb-setup.html` is written for an infrastructure engineer setting up
  their first one. "Pair" invites them to build two of something when one will do.
- **The public edition** proposed in `add-accounts-and-public-edition`, where a stranger with one
  server would have to be taught an internal word for a thing they already have a word for. That
  change lists it as an open question that bundles two things: what to call the level, and whether
  a public customer sees it at all. This change answers the first — the level is called a load
  balancer in both editions, and a per-edition rename is exactly what this change exists to
  prevent — and narrows that change's open question to the second, which is a decision about
  screens.

### Why "load balancer" rather than another collective noun

The alternatives all replace one counting word with another, or collide with vocabulary this
codebase has already spent:

- **"client set"** — `client` is taken twice and both meanings are load-bearing.
  `internal/httpapi/challenge.go` uses it throughout for the *ACME client* ("a client that leaves
  the aliasing to us", "clients retry cleanups"), and the OIDC configuration uses it for the
  *OAuth client* (`ACME_OIDC_CLIENT_ID`, `acmeservice-console`). A third meaning would make
  "which client?" a question in every review. `set` is also a counting word — vaguer, not better,
  and nobody says "my set".
- **"LB cluster"** — free in the codebase and the honest runner-up. Rejected because a cluster of
  one is odd, and "cluster" implies the nodes coordinate with each other, which this service
  neither models nor knows.
- **"group"** — spoken for. 134 occurrences in the backend, all identity-provider groups, and the
  role mapping is configured with `ACME_ADMIN_GROUP` and friends.

**"Load balancer"** removes the problem rather than restating it: it counts nothing. A customer
with two Traefik machines already says *"my load balancer"* and means the logical thing. The
members keep the name they already have — nodes — so the hierarchy reads as
"the load balancer `prod` has two nodes, `a` and `b`, and serves `demo.example.com`", and one
node is as natural a sentence as five.

### Why now

Nothing has shipped. The repository is one initial commit, `load_balancers` is created by the
unshipped `000001` migration rather than by a migration that would need a data move, and no
consumer outside this repository has ever called `/api/v1/lbpairs/:id`. The rename is 1,065
mechanical occurrences today. After a release it is those occurrences plus a migration, a
deprecation window on the API, and a docs page that has to explain both words.

## What Changes

Vocabulary only. No behaviour, no schema semantics, no endpoint that does something different
from what it did before.

| Where | Before | After |
| --- | --- | --- |
| Go type | `LBPair`, `LBPairID` | `LoadBalancer`, `LoadBalancerID` |
| Table | `lb_pairs` | `load_balancers` |
| Column | `lb_pair_id` | `load_balancer_id` |
| Routes | `/lbpairs/:id`, `/projects/:id/lbpairs` | `/loadbalancers/:id`, `/projects/:id/loadbalancers` |
| CLI | `--lbpair`, `project-slug/pair-slug` | `--loadbalancer`, `project-slug/loadbalancer-slug` |
| Console | `features/lbpair/`, `LbPairDetailComponent` | `features/loadbalancer/`, `LoadBalancerDetailComponent` |
| Prose | "load balancer pair", "LB pair" | "load balancer" |

`lb_nodes` keeps its name: a node was never mis-named, and `LBNode` reads correctly as a node of
a load balancer.

One requirement genuinely changes rather than being re-worded: the number of nodes a load
balancer may have is stated as one or more, which is what the code has always done and what the
name previously contradicted.

### Not in scope

- Any change to what the service does. Every endpoint, record and check behaves as before.
- Renaming `LBNode`, `lb_nodes`, or the `ACME_*` configuration variables.
- The `traefik-lb` container in the development stack — that is the customer-like load balancer
  of the acceptance rig, and its name was already right.

## Capabilities

### Modified Capabilities

- **service-inventory** — one requirement, *The inventory is projects, load balancers and
  domains*. A load balancer owns **one or more** nodes rather than zero or more, and a
  single-node load balancer is explicitly complete. That is the sentence the old name
  contradicted, and it is the only requirement whose meaning changes.

**No delta for admin-console or api-credentials.** Both were drafted and then withdrawn, and the
reason is worth recording because it is the same reason twice. The vocabulary in
`openspec/changes/add-acme-alias-service/specs/` is renamed *in place* (design R4), so by the
time anyone reads those capabilities they already say "load balancer" — a `## MODIFIED
Requirements` block restating the new noun would carry no normative content the base does not
already carry.

Worse, it would carry *less*. OpenSpec replaces a MODIFIED requirement wholesale when the change
is archived, so the withdrawn `api-credentials` delta — which said only that a key belongs to one
load balancer and that nodes share it — would have deleted the base requirement's allowance that
"a load balancer MAY have more than one active key, so that a key can be replaced without an
interruption". The withdrawn `admin-console` delta would likewise have deleted the base's
requirement that a user can *create, edit and remove* each level subject to their role, replacing
it with a sentence about what the screens are called. A rename that silently drops two real
requirements is a worse outcome than a rename with one delta.

## Impact

- **1,065 occurrences** across 95 files: Go, TypeScript, SQL, shell, HTML documentation and the
  specs themselves.
- **Two file moves**: `backend/internal/store/lbpair.go` → `load_balancer.go`, and
  `frontend/src/app/features/lbpair/` → `features/loadbalancer/`.
- **Migration `000001`** is edited in place rather than followed by a rename migration, because it
  has never been applied outside a development stack that is reset from empty.
- **The golden schema** (`backend/internal/store/schema.sql`) is regenerated; the diff is the
  table and column rename and nothing else, which is what makes it reviewable.
- **No API deprecation window**, because there is no consumer to deprecate for.
