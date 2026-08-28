# Design

Decisions are numbered `L1`, `L2`, … and referred to by number from `tasks.md`.

## L1. Which of the two objects survives, and under which name

The two rows are merged into one. The question is what it is called.

**`project` wins the name; `load_balancer` wins the contents.** The surviving table is `projects`,
and what it holds is everything `load_balancers` held.

**Because we do not operate a load balancer.** The nodes belong to the customer; we answer DNS
challenges so that *their* certificates can be issued. "Load balancer" names a piece of their
infrastructure, and it is the wrong word for anyone whose setup is a single host, a CDN origin or an
appliance — which is a large fraction of the people this service is for. The awkwardness already
shows in the documentation, where the object has to be explained before it can be used.

"Project" names a unit of *our* service — one set of nodes, domains and keys that belong together —
and it is a word we get to define rather than one borrowed from a category the customer may not be
in.

**Rejected: keeping both words by renaming projects to something else** (`workspace`, `environment`).
It preserves a distinction the whole change exists to delete, and every one of those words is another
noun a customer has to learn before they can publish a `CNAME`.

## L2. The migration is mostly a DROP, because the link already exists

The reason this is a small change rather than a rewrite: `tenancy-and-permissions` P2/P3 materialized
`project_id` onto every descendant so the row-level security predicate could be answered without a
join. `lb_nodes`, `aliases` and `domains` each carry `account_id`, `project_id` **and**
`load_balancer_id`.

So the parent link this change needs is already on every row. The migration drops
`load_balancer_id` and the `load_balancers` table, and re-points the foreign keys at `projects`.
Nothing has to be recomputed.

**The one place that is not mechanical:** a project holding two load balancers has to become two
projects, not one project with both sets of nodes. Merging would collide — alias labels and challenge
names are unique per load balancer, and two load balancers under one project may legitimately hold
the same domain name. The rule is one project per load balancer, with the project taking the load
balancer's slug.

There is no production data, so this is stated for correctness rather than executed: the migration
is written radically, per the standing instruction, and the rule above is what a data-carrying
deployment would need.

## L3. The quota's meaning changes underneath its name

`ProjectsPerPersonalAccount` is 5 (`personal-and-team-accounts` C13).

Before this change that bounded **folders**, each holding unlimited load balancers — so as an abuse
control it bounded very little, and its real job was done by per-domain billing and the activation
gate. After this change it bounds the thing that actually costs us something.

This is the sharp edge of the whole change, and it cuts in a direction that is easy to miss: **a
setting keeps its name and its value while its meaning tightens.** Somebody reading `5` before and
after would conclude nothing had happened.

Two consequences:

- **The number is probably wrong now** and should be revisited as part of this change rather than
  left to be discovered by the first personal-account user who wanted a staging environment. Five
  folders is not five load balancers.
- **The comment on the setting has to say what it now counts**, because the sentence in
  `config.yaml` describing "a personal account may hold this many projects" is true before and after
  and means different things.

## L4. The grant grammar loses an object type, and the subset rule gets simpler

`ObjectLoadBalancer` and `load_balancer.create` / `.update` / `.delete` leave
`internal/authz/vocab.go`. Grants naming a load balancer become grants naming a project.

This **simplifies** P10's subset rule rather than complicating it. A grant on a project used to imply
authority over the load balancers within it — one inheritance step, which is one place the "may I
grant this?" check had to reason about a level. That step no longer exists because the level does
not.

**Rejected: keeping `load_balancer` as an alias for `project` in the grammar** so old grants keep
parsing. Every stored grant is ours, there are no customers, and a grammar with two words for one
object type is precisely the kind of thing `000003` was written to remove when it dropped the
selector form. A migration that rewrites the rows and a check constraint that refuses the old word is
the version that cannot rot.

## L5. A project is shown with what is in it

A project row in the list reads as its name and **what it contains** — `4 nodes · 12 domains`.

This is not decoration, and it is what stops the collapse from making the list *less* informative
than it was. Before, a project's row could say "3 load balancers", which was a real answer to "what
is in here" even if the unit was a folder. Collapse the level naively and the row says only a name,
which is the least useful list in the product: the whole point of the merge is that a project is now
a concrete thing, and a list of concrete things should say how big each one is.

The two numbers are also the two questions somebody actually has. **Nodes** is "how much
infrastructure is this", and **domains** is "how much of my certificate estate depends on it" — which
is the one that decides how carefully to touch it.

They are counted in the query that lists projects, not fetched per row: a list of ten projects that
issues twenty-one queries is the shape that looks fine in development and falls over on the account
that has forty.

**A project with nothing in it says so** — "no nodes, no domains" rather than "0 · 0". A freshly
created project is the most common empty state in the product, and it is a state with an obvious next
action, so the row is where to say it.

## L6. One screen where there were two

`project-detail` and `loadbalancer-detail` merge, and the survivor is **`loadbalancer-detail`'s**
content under the project's name.

That is the asymmetry worth naming: the two screens are not equal partners. `loadbalancer-detail` is
the real screen — nodes, domains, the plan, the keys, the interactions — and `project-detail` is
mostly a list of links to it plus the usage panel. So the merge keeps the former whole and folds the
latter's genuinely-project-level pieces (usage, inventory history) into it.

The breadcrumb loses a crumb. `Projects › my-project › prod` becomes `Projects › prod`, which is the
visible form of the whole change.

## L7. The API paths move, and this is the last moment that is free

`/projects/:id/loadbalancers` and every `/loadbalancers/:id/...` become `/projects/:id/...`.

No deprecation window, no aliases, no `Sunset` header. There is no customer programming against this
contract, and adding a compatibility layer for zero callers would leave the codebase carrying two
route trees forever to protect nobody.

**This is the argument for the change's position in the sequence.** `swagger-ui` is about to render
this contract for readers; `account-invitations` is about to hand out roles written in the grant
grammar; `metering-and-credits` is about to bill against this shape. Each of those makes the collapse
more expensive, and none of them makes it less necessary.

## Risks / Trade-offs

- **A customer loses the grouping.** Somebody with `staging` and `production` under one project now
  has two projects and nothing that says they belong together. This is the change's real cost. The
  account is the grouping that remains, and a naming convention (`shop-staging`, `shop-production`)
  is what people will actually use — which is what they were mostly using the project name for
  anyway.
- **A quota tightens silently** (L3). Mitigated by revisiting the number here rather than later.
- **A large, wide diff.** Schema, grants, audit vocabulary, API, CLI, both consoles, the acceptance
  suite. Wide but shallow: most of it is deleting one level of indirection, and the compiler finds
  nearly all of it.

## Migration Plan

Radical, per the standing instruction that there is no production data.

1. Fold `load_balancers` into `projects`; drop `load_balancer_id` from `lb_nodes`, `aliases` and
   `domains`; re-point the foreign keys and the RLS policies at `projects`.
2. Drop `project_daily_stats.load_balancer_count`.
3. Remove the object type and its actions from the authorization and audit vocabularies; rewrite
   stored grants; add the check that refuses the old word.
4. Move the routes; regenerate the OpenAPI document's paths.
5. Merge the two console screens; add the summary counts (L5).
6. Revisit the project quota (L3).

Rollback is the previous migration; a partially rolled-out state has a console asking for
`/loadbalancers/...` against a service that no longer serves it, which fails loudly and immediately
rather than silently.

## Open Questions

- **What the quota should now be** (L3). It is a pricing decision, not an engineering one.
- **Whether `slug` uniqueness moves.** Slugs were unique per project for load balancers and per
  account for projects; with one object there is one answer, and per-account is the obvious one — but
  it means a customer who had `prod` under two projects has a collision to resolve.
- **Whether the summary should count anything else** (L5) — certificates observed, or challenges in
  the last day. Both are more interesting than a count and both cost a join against a table that
  grows; not now.
