package inventory

import (
	"context"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/authz"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/store"
)

// TestIntegrationAListingShowsOnlyWhatTheGrantsReach is the ceiling this change
// carried until now, closed.
//
// Until the predicate compiler, a listing was confined to the account and gated
// by a whole-account permission check — so a contractor granted ONE project saw
// every project the account had. The grants are now compiled into the WHERE
// clause (design P13), so a row the caller may not see is never returned by the
// database rather than filtered out afterwards.
func TestIntegrationAListingShowsOnlyWhatTheGrantsReach(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	admin := authz.CLISubject(sc.AccountID())

	mine, err := CreateProject(ctx, db, sc, admin, "mine", "Mine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProject(ctx, db, sc, admin, "theirs", "Theirs"); err != nil {
		t.Fatal(err)
	}

	// A contractor with one inheritable grant over one project, and nothing else.
	contractor := authz.Subject{
		AccountID: sc.AccountID(),
		Grants: []authz.Grant{{
			AccountID:   sc.AccountID(),
			Type:        authz.TypeProject,
			ObjectID:    &mine.ID,
			Action:      string(authz.ActionViewInventory),
			Inheritable: true,
		}},
	}

	got, total, err := ListProjectsPage(ctx, db, sc, contractor, "", 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != mine.ID {
		slugs := make([]string, 0, len(got))
		for _, p := range got {
			slugs = append(slugs, p.Slug)
		}
		t.Fatalf("listed %v, want only the granted project", slugs)
	}
	// The count runs the same predicate as the page. A total that included the
	// other project would leak its existence through a number, undoing what
	// omitting the row was protecting.
	if total != 1 {
		t.Fatalf("total = %d, want 1 — the count must use the same predicate as the page", total)
	}
}

// A subject holding no grant at all gets an empty page rather than an error.
// "You may see none of these" and "you may not ask" are different answers, and
// only the first is true of a listing.
func TestIntegrationAListingWithNoGrantsIsEmptyRatherThanRefused(t *testing.T) {
	db, sc := scopedDB(t)
	ctx := context.Background()
	if _, err := CreateProject(ctx, db, sc, authz.CLISubject(sc.AccountID()), "hidden", "Hidden"); err != nil {
		t.Fatal(err)
	}

	stranger := authz.Subject{AccountID: sc.AccountID()}
	got, total, err := ListProjectsPage(ctx, db, sc, stranger, "", 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 || total != 0 {
		t.Fatalf("listed %d of %d, want an empty page — an empty predicate must not read as no filter", len(got), total)
	}
}

// The compiler itself, over the shapes a query has to survive.
func TestCompilePredicate(t *testing.T) {
	account := model.NewID()
	project, lb := model.NewID(), model.NewID()
	grant := func(t authz.ObjectType, id *model.ID, action string, inheritable bool) authz.Grant {
		return authz.Grant{AccountID: account, Type: t, ObjectID: id, Action: action, Inheritable: inheritable}
	}

	for _, tc := range []struct {
		name    string
		subject authz.Subject
		target  authz.ObjectType
		wantAll bool
		wantNil bool
		column  string
	}{
		{
			name:    "unrestricted reaches everything",
			subject: authz.Subject{AccountID: account, Unrestricted: true},
			target:  authz.TypeProject,
			wantAll: true,
		},
		{
			name:    "a wildcard identifier reaches everything in the account",
			subject: authz.Subject{AccountID: account, Grants: []authz.Grant{grant(authz.TypeProject, nil, "inventory.view", false)}},
			target:  authz.TypeProject,
			wantAll: true,
		},
		{
			name:    "one project compiles to an id term",
			subject: authz.Subject{AccountID: account, Grants: []authz.Grant{grant(authz.TypeProject, &project, "inventory.view", false)}},
			target:  authz.TypeProject,
			column:  "id",
		},
		{
			name:    "an inheritable project grant compiles to project_id on a load balancer",
			subject: authz.Subject{AccountID: account, Grants: []authz.Grant{grant(authz.TypeProject, &project, "inventory.view", true)}},
			target:  authz.TypeLoadBalancer,
			column:  "project_id",
		},
		{
			name:    "a NON-inheritable project grant reaches no load balancer",
			subject: authz.Subject{AccountID: account, Grants: []authz.Grant{grant(authz.TypeProject, &project, "inventory.view", false)}},
			target:  authz.TypeLoadBalancer,
			wantNil: true,
		},
		{
			name:    "a grant for another action contributes nothing",
			subject: authz.Subject{AccountID: account, Grants: []authz.Grant{grant(authz.TypeProject, &project, "inventory.write", true)}},
			target:  authz.TypeProject,
			wantNil: true,
		},
		{
			name:    "a load balancer grant does not reach upward to its project",
			subject: authz.Subject{AccountID: account, Grants: []authz.Grant{grant(authz.TypeLoadBalancer, &lb, "inventory.view", true)}},
			target:  authz.TypeProject,
			wantNil: true,
		},
		{
			name:    "a subject with no account compiles to nothing",
			subject: authz.Subject{Grants: []authz.Grant{grant(authz.TypeProject, &project, "inventory.view", true)}},
			target:  authz.TypeProject,
			wantNil: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := authz.CompilePredicate(tc.subject, "inventory.view", tc.target)
			switch {
			case tc.wantAll:
				if !p.All {
					t.Fatalf("predicate = %s, want everything", p)
				}
			case tc.wantNil:
				if !p.Empty() {
					t.Fatalf("predicate = %s, want nothing", p)
				}
			default:
				if p.All || p.Empty() {
					t.Fatalf("predicate = %s, want a %s term", p, tc.column)
				}
				if len(p.Columns[tc.column]) != 1 {
					t.Fatalf("predicate = %s, want one %s term", p, tc.column)
				}
			}
		})
	}
}

var _ = store.ACL{}
