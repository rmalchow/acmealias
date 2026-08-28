# Tasks

> **The cheap part is already done for you.** `tenancy-and-permissions` P2/P3 materialized
> `project_id` onto every descendant so the RLS predicate needed no join — so `lb_nodes`, `aliases`
> and `domains` already carry the parent link this change needs. The migration mostly DROPS a column
> rather than rewriting relationships (design L2).

## 1. Schema

- [ ] 1.1 Fold `load_balancers` into `projects`: the surviving table is `projects` and it holds what `load_balancers` held (design L1)
- [ ] 1.2 Drop `load_balancer_id` from `lb_nodes`, `aliases` and `domains`, and re-point their foreign keys at `projects`
- [ ] 1.3 Re-point the RLS policies. They read `account_id` and `project_id`, both of which survive, so this is smaller than it looks — but assert it rather than assuming: RLS EMPTIES rather than fails, so a policy left naming a dropped column is a scopeless read that returns no rows and no error
- [ ] 1.4 Drop `project_daily_stats.load_balancer_count`
- [ ] 1.5 Decide slug uniqueness. It was per-project for load balancers and per-account for projects; with one object per-account is the obvious answer, and it means a customer with `prod` under two projects has a collision. See the open question
- [ ] 1.6 Write it radically — there is no production data. Record in the migration what a data-carrying deployment would need instead: ONE PROJECT PER LOAD BALANCER, taking the load balancer's slug, never merging two into one. Merging collides, because alias labels and challenge names are unique per load balancer and two under one project may hold the same domain (design L2)
- [ ] 1.7 Regenerate `internal/store/schema.sql` through the golden test and confirm `model_drift_test` passes

## 2. The model and the store

- [ ] 2.1 Delete `model.LoadBalancer`; move its fields and associations onto `model.Project`
- [ ] 2.2 Follow the compiler through `internal/store` and `internal/inventory`. Most of this change is deleting one level of indirection and the compiler finds nearly all of it
- [ ] 2.3 Keep `store.Scope` and `store.CrossAccount` exactly as they are. This change removes a level; it does not touch how a scope is constructed

## 3. Authorization and audit

- [ ] 3.1 Remove `ObjectLoadBalancer` and its create/update/delete actions from `internal/authz/vocab.go` (design L4)
- [ ] 3.2 Rewrite stored grants naming a load balancer to name the project it became
- [ ] 3.3 Add the check constraint that REFUSES the old object type, rather than accepting and ignoring it. A grammar with two words for one object is what `000003` was written to remove; tighten the check BEFORE dropping anything, so a row that should not exist aborts loudly
- [ ] 3.4 Simplify the subset rule where it reasoned about project-implies-load-balancer. That inheritance step is gone because the level is (design L4)
- [ ] 3.5 Remove the load balancer actions from the audit vocabulary and confirm the project actions cover what they covered

## 4. The API

- [ ] 4.1 Move `/projects/:id/loadbalancers` and every `/loadbalancers/:id/...` to `/projects/:id/...`
- [ ] 4.2 Add NO compatibility aliases and no deprecation window. There is no customer programming against this contract, and a compatibility layer for zero callers is two route trees carried forever to protect nobody (design L7)
- [ ] 4.3 Update `internal/httpapi/openapi.yaml`. The conformance test fails until the console and the document agree, which is the mechanism that makes this task self-checking
- [ ] 4.4 Update the CLI

## 5. Counting what is in a project

- [ ] 5.1 Return the node count and the domain count with each project in the listing (design L5)
- [ ] 5.2 Count them IN the listing query. Ten projects issuing twenty-one queries is the shape that looks fine in development and falls over on the account with forty
- [ ] 5.3 Report a project holding neither as EMPTY rather than as two zeroes — a freshly created project is the most common empty state in the product, and it has an obvious next action

## 6. The console

- [ ] 6.1 Merge `project-detail` and `loadbalancer-detail`. The survivor is loadbalancer-detail's CONTENT under the project's name: it is the real screen, and project-detail was mostly links to it (design L6)
- [ ] 6.2 Fold project-detail's genuinely project-level pieces — usage, inventory history — into the merged screen
- [ ] 6.3 Show the counts in the project list, and the empty state where there are none (design L5)
- [ ] 6.4 Drop a crumb from the breadcrumb: `Projects › my-project › prod` becomes `Projects › prod`, which is the visible form of the whole change
- [ ] 6.5 Follow the routes: `/loadbalancers/:id` disappears
- [ ] 6.6 Put shared components in `shared` unless there is a clear reason otherwise

## 7. The quota, whose meaning changed

- [ ] 7.1 Revisit the number. Five FOLDERS bounded almost nothing; five projects bounds the real thing, and a personal account that wanted a staging environment is now at 2 of 5 (design L3)
- [ ] 7.2 Rewrite the comment in `config.yaml` and on `Config.ProjectsPerPersonalAccount` to say what it now counts. The old sentence is true before and after and means different things, which is the worst kind of stale documentation
- [ ] 7.3 Say in the change what the number was and what it became, so the tightening is recorded rather than discovered

## 8. Tests

- [ ] 8.1 Follow the acceptance suite through. It walks project → load balancer → domain in several places, and `onboardDomain` and the plan tests are where the shape is most visible
- [ ] 8.2 Assert an RLS regression cannot hide here: a read scoped to one account must return nothing for another's project, and it must do so because the policy matched and not because a column vanished (task 1.3)
- [ ] 8.3 Assert a grant naming the removed object type is refused
- [ ] 8.4 Assert the listing's query count does not grow with the number of projects (task 5.2)
