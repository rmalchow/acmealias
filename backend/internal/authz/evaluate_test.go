package authz

import (
	"math/rand/v2"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The evaluator's table. Every combination of wildcard, identity, selector and
// inheritance that the spec names, plus the ones it names by exclusion.

var (
	acct    = model.NewID()
	otherAc = model.NewID()
	projA   = model.NewID()
	projB   = model.NewID()
	lbA     = model.NewID()
	lbB     = model.NewID()
	nodeA   = model.NewID()
)

func id(v model.ID) *model.ID { return &v }

// object builders, so a test row reads as what it is about rather than as five
// identifier fields.
func lbObject(account, project, lb model.ID) Object {
	return Object{Type: TypeLoadBalancer, ID: lb, AccountID: account, ProjectID: project, LoadBalancerID: lb}
}

func nodeObject(account, project, lb, node model.ID) Object {
	return Object{Type: TypeNode, ID: node, AccountID: account, ProjectID: project, LoadBalancerID: lb}
}

func projectObject(account, project model.ID) Object {
	return Object{Type: TypeProject, ID: project, AccountID: account, ProjectID: project}
}

func TestCanCoversEveryCombination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		grants []Grant
		action string
		object Object
		want   bool
	}{
		{
			name:   "a grant on one object permits that object",
			grants: []Grant{{AccountID: acct, Type: TypeLoadBalancer, ObjectID: id(lbA), Action: "inventory.write"}},
			action: "inventory.write",
			object: lbObject(acct, projA, lbA),
			want:   true,
		},
		{
			name:   "a grant on one object permits nothing else",
			grants: []Grant{{AccountID: acct, Type: TypeLoadBalancer, ObjectID: id(lbA), Action: "inventory.write"}},
			action: "inventory.write",
			object: lbObject(acct, projA, lbB),
			want:   false,
		},
		{
			name:   "a wildcard action permits every action on the named object",
			grants: []Grant{{AccountID: acct, Type: TypeProject, ObjectID: id(projA), Action: Wildcard}},
			action: "anything.at.all",
			object: projectObject(acct, projA),
			want:   true,
		},
		{
			name:   "a wildcard identifier covers the account it belongs to",
			grants: []Grant{{AccountID: acct, Type: TypeLoadBalancer, Action: "inventory.write"}},
			action: "inventory.write",
			object: lbObject(acct, projA, lbA),
			want:   true,
		},
		{
			name:   "a wildcard does not reach another account",
			grants: []Grant{{AccountID: acct, Type: TypeLoadBalancer, Action: "inventory.write"}},
			action: "inventory.write",
			object: lbObject(otherAc, projA, lbA),
			want:   false,
		},
		{
			name:   "an inheritable grant reaches contained objects",
			grants: []Grant{{AccountID: acct, Type: TypeProject, ObjectID: id(projA), Action: "inventory.write", Inheritable: true}},
			action: "inventory.write",
			object: nodeObject(acct, projA, lbA, nodeA),
			want:   true,
		},
		{
			name:   "a non-inheritable grant does not reach contained objects",
			grants: []Grant{{AccountID: acct, Type: TypeProject, ObjectID: id(projA), Action: "inventory.write"}},
			action: "inventory.write",
			object: lbObject(acct, projA, lbA),
			want:   false,
		},
		{
			name:   "inheritance does not reach upward",
			grants: []Grant{{AccountID: acct, Type: TypeLoadBalancer, ObjectID: id(lbA), Action: "inventory.write", Inheritable: true}},
			action: "inventory.write",
			object: projectObject(acct, projA),
			want:   false,
		},
		{
			name:   "an inheritable grant on one project does not reach another",
			grants: []Grant{{AccountID: acct, Type: TypeProject, ObjectID: id(projA), Action: "inventory.write", Inheritable: true}},
			action: "inventory.write",
			object: lbObject(acct, projB, lbB),
			want:   false,
		},
		{
			name:   "an unmatched action is refused",
			grants: []Grant{{AccountID: acct, Type: TypeAny, Action: "inventory.view"}},
			action: "inventory.write",
			object: lbObject(acct, projA, lbA),
			want:   false,
		},
		{
			name:   "a wildcard type with a wildcard identifier is everything in the account",
			grants: []Grant{{AccountID: acct, Type: TypeAny, Action: Wildcard}},
			action: "inventory.write",
			object: nodeObject(acct, projA, lbA, nodeA),
			want:   true,
		},
		{
			name: "authority is the union, and order does not matter",
			grants: []Grant{
				{AccountID: acct, Type: TypeLoadBalancer, ObjectID: id(lbB), Action: "inventory.write"},
				{AccountID: acct, Type: TypeLoadBalancer, ObjectID: id(lbA), Action: "inventory.write"},
			},
			action: "inventory.write",
			object: lbObject(acct, projA, lbA),
			want:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Subject{AccountID: acct, Grants: tc.grants}
			if got := Can(s, tc.action, tc.object); got != tc.want {
				t.Fatalf("Can = %v, want %v", got, tc.want)
			}
		})
	}
}

// The unrestricted role is every action on every object of ITS account, and
// nothing at all outside it — the second half is the one worth a test, because
// "unrestricted" reads as "no limits".
func TestUnrestrictedIsStillAnchored(t *testing.T) {
	s := Subject{AccountID: acct, Unrestricted: true}
	if !Can(s, "anything", lbObject(acct, projA, lbA)) {
		t.Fatal("the unrestricted role must permit every action in its own account")
	}
	if Can(s, "inventory.view", lbObject(otherAc, projA, lbA)) {
		t.Fatal("the unrestricted role reached another account")
	}
}

// A subject with no account matches nothing, so a zero Subject cannot be a
// skeleton key.
func TestZeroSubjectPermitsNothing(t *testing.T) {
	if Can(Subject{}, "inventory.view", lbObject(acct, projA, lbA)) {
		t.Fatal("a subject with no anchor permitted something")
	}
}

// Adding a grant never removes authority: the spec's allow-only property, tested
// rather than asserted, because a deny rule sneaking in would be invisible until
// someone lost access they used to have.
func TestGrantsAreOnlyAdditive(t *testing.T) {
	base := []Grant{{AccountID: acct, Type: TypeLoadBalancer, ObjectID: id(lbA), Action: "inventory.write"}}
	extra := Grant{AccountID: acct, Type: TypeProject, ObjectID: id(projB), Action: "inventory.view", Inheritable: true}

	before := Subject{AccountID: acct, Grants: base}
	after := Subject{AccountID: acct, Grants: append(append([]Grant{}, base...), extra)}

	obj := lbObject(acct, projA, lbA)
	if !Can(before, "inventory.write", obj) || !Can(after, "inventory.write", obj) {
		t.Fatal("adding a grant removed authority")
	}
}

// TestInheritableGrantMatchesExactlyWhatItContains is the property design P3's
// inheritance rests on: an inheritable grant naming an object matches every
// object that object contains, and nothing else. Randomized over a synthetic
// tree, so it covers shapes nobody wrote a row for.
func TestInheritableGrantMatchesExactlyWhatItContains(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))

	type node struct {
		obj    Object
		inProj model.ID
		inLB   model.ID
	}

	for range 200 {
		projects := []model.ID{model.NewID(), model.NewID(), model.NewID()}
		var world []node
		for _, p := range projects {
			for range 1 + r.IntN(3) {
				lb := model.NewID()
				world = append(world, node{obj: lbObject(acct, p, lb), inProj: p, inLB: lb})
				for range r.IntN(3) {
					n := model.NewID()
					world = append(world, node{obj: nodeObject(acct, p, lb, n), inProj: p, inLB: lb})
				}
			}
			world = append(world, node{obj: projectObject(acct, p), inProj: p})
		}

		target := projects[r.IntN(len(projects))]
		s := Subject{AccountID: acct, Grants: []Grant{
			{AccountID: acct, Type: TypeProject, ObjectID: id(target), Action: "inventory.write", Inheritable: true},
		}}

		for _, n := range world {
			// "Contained by the named project" means: in that project, and not
			// the project row itself — inheritance reaches down, never at the
			// object it names through containment.
			contained := n.inProj == target && n.obj.Type != TypeProject
			named := n.obj.Type == TypeProject && n.obj.ID == target
			want := contained || named
			if got := Can(s, "inventory.write", n.obj); got != want {
				t.Fatalf("object %s of type %s in project %s: Can = %v, want %v",
					n.obj.ID, n.obj.Type, n.inProj, got, want)
			}
		}
	}
}
