package authz

import (
	"slices"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The three properties that went missing when the global-role permission matrix
// was deleted with the internal edition (design C10).
//
// The matrix itself is gone and rightly so — equivalence_test.go pins the seeded
// roles against it and that is what replaces it. What has no successor is the
// evaluator's own behaviour at the edges the matrix used to cover incidentally:
// what an unrecognized action does, whether two roles add up, and what
// PermittedInAccount means by "account-wide". All three still hold; none of them
// was asserted anywhere, so nothing would have noticed them stopping.

// TestCanUnknownActionFailsClosed is the successor to the deleted test of the
// same name, and the property it pins CHANGED SHAPE rather than going away —
// which is why it is worth more now, not less.
//
// It used to be a lookup: an action outside the matrix found no row and returned
// false. There is no matrix now, so "unknown" is not a category the evaluator
// has; an action is a string compared against g.Action. The property that has to
// survive that is that a grant confers ONE action and never a family of them, so
// a typo, a renamed constant or a caller inventing a verb is refused rather than
// silently permitted by something near it.
//
// The unrestricted subject is the deliberate exception and is asserted as such:
// it passes any action string, because "every action on every object of this
// account" cannot be enumerated without the flag becoming a list somebody can
// edit down (see Subject.Unrestricted).
func TestCanUnknownActionFailsClosed(t *testing.T) {
	account := model.NewID()
	lb := model.NewID()
	object := Object{Type: TypeLoadBalancer, ID: lb, AccountID: account, LoadBalancerID: lb}

	granted := Subject{AccountID: account, Grants: []Grant{
		{AccountID: account, Type: TypeAny, Action: string(ActionInventoryWrite)},
	}}
	for _, action := range []string{
		"",                    // a caller that forgot to pass one
		"inventory",           // a prefix of the granted action
		"inventory.write.all", // the granted action as a prefix
		"inventory.WRITE",     // the same verb, differently cased
		"nonsense.verb",       // an action this service has never had
		"*",                   // the wildcard, which is a GRANT spelling and not an action
	} {
		if Can(granted, action, object) {
			t.Errorf("Can(subject holding %q, %q) = true; an action is matched exactly or not at all",
				ActionInventoryWrite, action)
		}
	}
	if !Can(granted, string(ActionInventoryWrite), object) {
		t.Fatal("the granted action itself was refused; the table above is asserting nothing")
	}

	unrestricted := Subject{AccountID: account, Unrestricted: true}
	if !Can(unrestricted, "nonsense.verb", object) {
		t.Error("an unrestricted subject was refused an unrecognized action; " +
			"Unrestricted is deliberately not a list of actions, so it cannot answer 'not that one'")
	}
}

// TestCanRolesAreAdditive is the successor to the deleted test of that name, at
// the evaluator rather than at the role table.
//
// Authority is a union over grants and nothing else: no grant removes what
// another confers, and the order they were written in is irrelevant. That is the
// property that makes "give this person a second role" a safe sentence, and it
// is the one a deny rule would take away.
func TestCanRolesAreAdditive(t *testing.T) {
	account := model.NewID()
	project := model.NewID()
	lb := model.NewID()
	object := Object{Type: TypeLoadBalancer, ID: lb, AccountID: account, ProjectID: project, LoadBalancerID: lb}

	fromOneRole := Grant{AccountID: account, Type: TypeLoadBalancer, ObjectID: &lb, Action: string(ActionInventoryWrite)}
	fromAnother := Grant{AccountID: account, Type: TypeProject, ObjectID: &project, Action: string(ActionKeyWrite), Inheritable: true}

	both := Subject{AccountID: account, Grants: []Grant{fromOneRole, fromAnother}}
	reversed := Subject{AccountID: account, Grants: []Grant{fromAnother, fromOneRole}}

	for name, s := range map[string]Subject{"as written": both, "reversed": reversed} {
		if !Can(s, string(ActionInventoryWrite), object) {
			t.Errorf("%s: the first role's grant was lost when a second was added", name)
		}
		if !Can(s, string(ActionKeyWrite), object) {
			t.Errorf("%s: the second role's grant was lost beside the first", name)
		}
		if Can(s, string(ActionProjectAdmin), object) {
			t.Errorf("%s: two roles conferred an action neither of them names", name)
		}
	}
}

// TestPermittedInAccountIsAccountWide pins what /api/v1/me answers, and in
// particular the distinction the console renders its shell from.
//
// A grant on ONE project is real authority and is deliberately ABSENT here: this
// question is "what may you do on anything in this account", which is what the
// old global role names meant and all they could mean. PermittedOn answers the
// per-screen question and would report that grant for that project. Conflating
// them renders a shell button that is right on average and wrong on every row —
// and nothing else in the tree asserts the difference, so this is where it is
// held.
func TestPermittedInAccountIsAccountWide(t *testing.T) {
	account := model.NewID()
	project := model.NewID()

	onOneProject := Subject{AccountID: account, Grants: []Grant{
		{AccountID: account, Type: TypeProject, ObjectID: &project, Action: string(ActionInventoryWrite), Inheritable: true},
	}}
	if got := PermittedInAccount(onOneProject); len(got) != 0 {
		t.Errorf("PermittedInAccount(grant on one project) = %v, want none: "+
			"authority over one project is not authority over the account", got)
	}

	accountWide := Subject{AccountID: account, Grants: []Grant{
		{AccountID: account, Type: TypeAny, Action: string(ActionInventoryWrite)},
		{AccountID: account, Type: TypeAny, Action: string(ActionViewInventory)},
	}}
	got := PermittedInAccount(accountWide)
	for _, want := range []Action{ActionInventoryWrite, ActionViewInventory} {
		if !slices.Contains(got, string(want)) {
			t.Errorf("PermittedInAccount = %v, missing the account-wide grant %q", got, want)
		}
	}
	if slices.Contains(got, string(ActionKeyWrite)) {
		t.Errorf("PermittedInAccount = %v, includes %q which was never granted", got, ActionKeyWrite)
	}
	// project.admin is governed by MEMBERSHIP of the unrestricted role rather
	// than by a grant (design P14), so no grant can produce it and the flag
	// always does.
	if slices.Contains(got, string(ActionProjectAdmin)) {
		t.Errorf("PermittedInAccount = %v, includes %q, which no grant may confer", got, ActionProjectAdmin)
	}
	if !slices.Contains(PermittedInAccount(Subject{AccountID: account, Unrestricted: true}), string(ActionProjectAdmin)) {
		t.Error("an unrestricted subject was not reported as permitted to administer projects")
	}

	// The zero subject holds nothing, and the account-anchor check in Can is
	// what makes that true rather than an empty grant list.
	if got := PermittedInAccount(Subject{}); len(got) != 0 {
		t.Errorf("PermittedInAccount(zero subject) = %v, want none", got)
	}
}
