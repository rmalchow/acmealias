## Why

### There are three levels and the middle one holds nothing

Today's inventory is `account → project → load balancer → (nodes, domains, aliases, API keys)`.

Here is the whole of `projects`, from `internal/model/model.go`:

```go
type Project struct {
    ID        ID
    AccountID ID
    Slug      string
    Name      string
    CreatedAt time.Time

    LoadBalancers []LoadBalancer
}
```

A slug, a name, and a list of children. Every actual thing a customer configures — the nodes, the
domains, the aliases, the API keys, the plan, the challenge interactions — hangs off the **load
balancer**, one level further down.

So a project is a folder. It carries no configuration, enforces no invariant, and answers no
question. What it does is cost a level: a customer creates a project, is shown an empty screen,
creates a load balancer inside it, and only then reaches something they can do anything with. Two
objects have to be named before the product does anything, and one of the two names is never used
again.

### The account is what the folder was for

When the hierarchy was designed there was no account. A project was the only way to keep one
customer's inventory separate from another's, and grouping several load balancers under a heading was
the only grouping there was.

`personal-and-team-accounts` gave that job to the **account**: it is the billing entity, the
permission boundary, the thing a session selects, and the thing a person switches between. Everything
a project was reached for, the account now does better — and what remains is a level that exists
because it used to be needed.

### The name was also wrong

Worth saying, since this change gets to pick which of the two names survives.

**We do not operate a load balancer.** The nodes are the customer's machines; we answer DNS
challenges on their behalf. "Load balancer" describes *their* infrastructure, which is why the object
is awkward to talk about in our own documentation — and why a customer running something that is not
a load balancer at all (a single host, a CDN origin, an appliance) has to squint at the word before
using it.

"Project" describes a unit of *our* service: one set of nodes, domains and keys that belong together.
So the collapse also settles the naming, and it settles it in favour of the word that is ours to
define.

### It is cheap exactly now, and never again

Two facts make this a small change today:

- **Every descendant already carries `project_id`.** It was materialized on `lb_nodes`, `aliases`,
  `domains` and the rest so the row-level security predicate could be answered without a join
  (`tenancy-and-permissions` P2/P3). So the parent link this change needs already exists on every
  row — the migration mostly *drops* a column rather than rewriting relationships.
- **There is no production data and no customer programming against the contract.** The API path
  change is a rename, not a deprecation; the grant grammar change rewrites rows nobody depends on.

Both of those stop being true at the first customer. `swagger-ui` is about to put the contract in
front of readers, `account-invitations` is about to hand out roles written against the grant grammar,
and `metering-and-credits` is about to bill against this shape.

## What Changes

### The shape

```
  before                              after
  ──────                              ─────
  account                             account
    └── project        (slug, name)     └── project   (slug, name, and everything below)
          └── load balancer                   ├── nodes
                ├── nodes                     ├── domains
                ├── domains                   ├── aliases
                ├── aliases                   └── API keys
                └── API keys
```

A project becomes what a load balancer is. The screen a customer already knows — nodes, domains, the
plan, the keys — is unchanged; it simply sits one level up, and the empty folder above it is gone.

### What a customer with several load balancers does

They get several projects. `staging` and `production` were two load balancers under one project; they
become two projects.

Nothing about their configuration moves, and the thing they lose is the heading that said those two
belonged together. That is a real loss and it is the change's actual cost — recorded in the design
rather than waved past.

### The quota means something different, and that is the sharp edge

`ProjectsPerPersonalAccount` is 5. Before, that was five *folders*, each holding as many load
balancers as you liked — so the quota bounded almost nothing. After, it bounds the real thing.

A personal account that had one project and six load balancers is, in the new shape, six projects and
over quota. There is no production data, so nobody is actually caught by this — but the SETTING's
meaning changes underneath its name, and the number probably has to change with it.

### The grant grammar loses an object type

`load_balancer` disappears from `internal/authz/vocab.go`, along with its create, update and delete
actions. Grants naming a load balancer become grants naming a project.

This simplifies the subset rule rather than complicating it: a grant on a project used to imply
authority over the load balancers inside it, which is one inheritance step that no longer exists.

### Everything else that names the level

RLS predicates and the materialized columns, the audit vocabulary, `project_daily_stats`'s
`load_balancer_count`, the API routes (`/loadbalancers/:id/...` becomes `/projects/:id/...`), the
published OpenAPI document, the CLI, and the console — where `project-detail` and
`loadbalancer-detail` become one screen.

## Capabilities

- `inventory` — a project holds nodes, domains, aliases and keys directly
- `identity-and-roles` — the grant grammar names projects and no longer names load balancers
- `admin-console` — one screen where there were two
- `audit-trail` — the load balancer actions go, and the project actions take their place

## Impact

- **Schema** — `load_balancers` dropped, `load_balancer_id` dropped from five tables. Radical
  migration; no production data
- **The published contract** — every `/loadbalancers/...` path moves. A rename while nobody is
  programming against it
- **Grants** — one object type removed, existing rows rewritten
- **Both consoles, the CLI, the acceptance suite** — every place that walks two levels now walks one
