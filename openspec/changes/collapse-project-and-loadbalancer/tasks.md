# Tasks

> **The cheap part is already done for you.** `tenancy-and-permissions` P2/P3 materialized
> `project_id` onto every descendant so the RLS predicate needed no join — so `lb_nodes`, `aliases`
> and `domains` already carry the parent link this change needs. The migration mostly DROPS a column
> rather than rewriting relationships (design L2).

## 1. Schema

- [x] 1.1 Fold `load_balancers` into `projects`: the surviving table is `projects` and it holds what `load_balancers` held (design L1)
- [x] 1.2 Drop `load_balancer_id` from `lb_nodes`, `aliases` and `domains`, and re-point their foreign keys at `projects`
- [x] 1.3 Re-point the RLS policies. They read `account_id` and `project_id`, both of which survive, so this is smaller than it looks — but assert it rather than assuming: RLS EMPTIES rather than fails, so a policy left naming a dropped column is a scopeless read that returns no rows and no error
- [x] 1.4 Drop `project_daily_stats.load_balancer_count`
- [ ] 1.5 **Left open.** `projects.slug` is unchanged — globally unique, as it already was — and no load balancer slug survives to collide with it. Deciding whether it should become per-account is a decision about what a slug IS, and nothing in this change forces it. Decide slug uniqueness. It was per-project for load balancers and per-account for projects; with one object per-account is the obvious answer, and it means a customer with `prod` under two projects has a collision. See the open question
- [x] 1.6 Write it radically — there is no production data. Record in the migration what a data-carrying deployment would need instead: ONE PROJECT PER LOAD BALANCER, taking the load balancer's slug, never merging two into one. Merging collides, because alias labels and challenge names are unique per load balancer and two under one project may hold the same domain (design L2)
- [x] 1.7 Regenerate `internal/store/schema.sql` through the golden test and confirm `model_drift_test` passes

## 2. The model and the store

- [x] 2.1 Delete `model.LoadBalancer`; move its fields and associations onto `model.Project`
- [x] 2.2 Follow the compiler through `internal/store` and `internal/inventory`. Most of this change is deleting one level of indirection and the compiler finds nearly all of it
- [x] 2.3 Keep `store.Scope` and `store.CrossAccount` exactly as they are. This change removes a level; it does not touch how a scope is constructed

## 3. Authorization and audit

- [x] 3.1 Remove `ObjectLoadBalancer` and its create/update/delete actions from `internal/authz/vocab.go` (design L4)
- [x] 3.2 Rewrite stored grants naming a load balancer to name the project it became
- [x] 3.3 Add the check constraint that REFUSES the old object type, rather than accepting and ignoring it. A grammar with two words for one object is what `000003` was written to remove; tighten the check BEFORE dropping anything, so a row that should not exist aborts loudly
- [x] 3.4 Simplify the subset rule where it reasoned about project-implies-load-balancer. That inheritance step is gone because the level is (design L4)
- [x] 3.5 Remove the load balancer actions from the audit vocabulary and confirm the project actions cover what they covered

## 4. The API

- [x] 4.1 Move `/projects/:id/loadbalancers` and every `/loadbalancers/:id/...` to `/projects/:id/...`
- [x] 4.2 Add NO compatibility aliases and no deprecation window. There is no customer programming against this contract, and a compatibility layer for zero callers is two route trees carried forever to protect nobody (design L7)
- [x] 4.3 Update `internal/httpapi/openapi.yaml`. The conformance test fails until the console and the document agree, which is the mechanism that makes this task self-checking
- [x] 4.4 Update the CLI

## 5. Counting what is in a project

- [x] 5.1 Return the node count and the domain count with each project in the listing (design L5)
- [x] 5.2 Count them IN the listing query. Ten projects issuing twenty-one queries is the shape that looks fine in development and falls over on the account with forty
- [x] 5.3 Report a project holding neither as EMPTY rather than as two zeroes — a freshly created project is the most common empty state in the product, and it has an obvious next action

## 6. The console

- [x] 6.1 Merge `project-detail` and `loadbalancer-detail`. The survivor is loadbalancer-detail's CONTENT under the project's name: it is the real screen, and project-detail was mostly links to it (design L6)
- [x] 6.2 Fold project-detail's genuinely project-level pieces — usage, inventory history — into the merged screen
- [x] 6.3 Show the counts in the project list, and the empty state where there are none (design L5)
- [x] 6.4 Drop a crumb from the breadcrumb: `Projects › my-project › prod` becomes `Projects › prod`, which is the visible form of the whole change
- [x] 6.5 Follow the routes: `/loadbalancers/:id` disappears
- [ ] 6.6 **Not audited.** The merged screen's parts moved from `features/loadbalancer/` into `features/projects/` beside the component that uses them; nothing was promoted to `shared`, and no sweep for wrongly-placed components was made. Put shared components in `shared` unless there is a clear reason otherwise

## 7. The quota, whose meaning changed

- [ ] 7.1 **Left open — it is the change's own open question.** The number is still 5 and the comments now say what it counts (7.2). Revisit the number. Five FOLDERS bounded almost nothing; five projects bounds the real thing, and a personal account that wanted a staging environment is now at 2 of 5 (design L3)
- [x] 7.2 Rewrite the comment in `config.yaml` and on `Config.ProjectsPerPersonalAccount` to say what it now counts. The old sentence is true before and after and means different things, which is the worst kind of stale documentation
- [ ] 7.3 Blocked on 7.1: the number has not changed, so there is nothing yet to record beyond the meaning shift, which `config.yaml` and `Config.ProjectsPerPersonalAccount` now carry. Say in the change what the number was and what it became, so the tightening is recorded rather than discovered

## 8. Tests

- [ ] 8.1 **Rewritten but NOT RUN** — the suite needs the whole stack and must run alone. It compiles under `-tags acceptance`, `demoProd` became `demoProject`, and `development/scripts/seed.sh` seeds one project where it seeded a project and a load balancer. Follow the acceptance suite through. It walks project → load balancer → domain in several places, and `onboardDomain` and the plan tests are where the shape is most visible
- [x] 8.2 Assert an RLS regression cannot hide here: a read scoped to one account must return nothing for another's project, and it must do so because the policy matched and not because a column vanished (task 1.3)
- [x] 8.3 Assert a grant naming the removed object type is refused
- [x] 8.4 Assert the listing's query count does not grow with the number of projects (task 5.2)
