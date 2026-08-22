package authz

import (
	"errors"
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// Pattern containment, exhaustively. The table is the specification: every row
// is a claim someone will otherwise have to re-derive from the code.
func TestPatternContains(t *testing.T) {
	for _, tc := range []struct {
		outer, inner string
		want         bool
		why          string
	}{
		{"*.b", "*.a.b", true, "a subtree is inside a wider subtree"},
		{"*.b", "a.b", true, "an exact name is inside the subtree above it"},
		{"*.a.b", "*.b", false, "a wider subtree is not inside a narrower one"},
		{"*.a.b", "a.b", false, "a pattern does not contain its own apex"},
		{"a.b", "a.b", true, "a name contains itself"},
		{"a.b", "*.a.b", false, "an exact name does not contain a subtree"},
		{"a.b", "c.b", false, "unrelated names contain nothing of each other"},
		{"*.b", "*.b", true, "a subtree contains itself"},
		{"*.example.com", "www.example.com", true, "the ordinary case"},
		{"*.example.com", "example.com", false, "the apex is excluded, which is what protects NS and MX"},
		{"*.example.com", "a.b.c.example.com", true, "a wildcard label spans several labels"},
		{"*.EXAMPLE.com", "www.example.COM", true, "comparison is case-insensitive"},
		{"*.example.com.", "www.example.com", true, "a trailing dot is not a difference"},
	} {
		if got := PatternContains(tc.outer, tc.inner); got != tc.want {
			t.Errorf("PatternContains(%q, %q) = %v, want %v — %s", tc.outer, tc.inner, got, tc.want, tc.why)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"*.staging.example.com", "web.staging.example.com", true},
		{"*.staging.example.com", "db.web.staging.example.com", true},
		{"*.staging.example.com", "staging.example.com", false},
		{"*.staging.example.com", "example.com", false},
		{"*.staging.example.com", "webstaging.example.com", false},
		{"www.example.com", "www.example.com", true},
		{"www.example.com", "other.example.com", false},
	} {
		if got := MatchPattern(tc.pattern, tc.name); got != tc.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// Interior wildcards are refused at write time rather than given a meaning.
func TestValidatePatternRefusesInteriorWildcards(t *testing.T) {
	for _, p := range []string{"web-*.example.com", "*.web.*.example.com", "web.*.example.com", "*x.example.com", ""} {
		if err := ValidatePattern(p); err == nil {
			t.Errorf("ValidatePattern(%q) = nil, want a refusal", p)
		}
	}
	for _, p := range []string{"*.example.com", "www.example.com", "*.a.b.c.example.com"} {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", p, err)
		}
	}
}

func TestCoversSingleGrant(t *testing.T) {
	a := model.NewID()
	p1, lb1, lb2 := model.NewID(), model.NewID(), model.NewID()

	g := func(mod func(*Grant)) Grant {
		out := Grant{AccountID: a, Type: TypeLoadBalancer, Action: "inventory.write"}
		mod(&out)
		return out
	}

	for _, tc := range []struct {
		name           string
		held, proposed Grant
		want           bool
	}{
		{
			name:     "a wildcard action covers a specific one",
			held:     g(func(x *Grant) { x.Action = Wildcard }),
			proposed: g(func(x *Grant) {}),
			want:     true,
		},
		{
			name:     "a specific action does not cover a wildcard",
			held:     g(func(x *Grant) {}),
			proposed: g(func(x *Grant) { x.Action = Wildcard }),
			want:     false,
		},
		{
			name:     "an inheritable project grant covers a load balancer grant beneath it",
			held:     Grant{AccountID: a, Type: TypeProject, Action: Wildcard, Inheritable: true},
			proposed: g(func(x *Grant) { x.ObjectID = id(lb1) }),
			want:     true,
		},
		{
			name:     "a non-inheritable project grant covers nothing beneath it",
			held:     Grant{AccountID: a, Type: TypeProject, ObjectID: id(p1), Action: Wildcard},
			proposed: g(func(x *Grant) { x.ObjectID = id(lb1) }),
			want:     false,
		},
		{
			name:     "the same object covers itself",
			held:     g(func(x *Grant) { x.ObjectID = id(lb1) }),
			proposed: g(func(x *Grant) { x.ObjectID = id(lb1) }),
			want:     true,
		},
		{
			name:     "one object does not cover another",
			held:     g(func(x *Grant) { x.ObjectID = id(lb1) }),
			proposed: g(func(x *Grant) { x.ObjectID = id(lb2) }),
			want:     false,
		},
		{
			name:     "one object does not cover a wildcard",
			held:     g(func(x *Grant) { x.ObjectID = id(lb1) }),
			proposed: g(func(x *Grant) {}),
			want:     false,
		},
		{
			name:     "a non-inheritable holding does not cover an inheritable proposal of the same object",
			held:     g(func(x *Grant) { x.ObjectID = id(lb1) }),
			proposed: g(func(x *Grant) { x.ObjectID = id(lb1); x.Inheritable = true }),
			want:     false,
		},
		{
			name:     "a grant in another account is never covered",
			held:     g(func(x *Grant) { x.Action = Wildcard }),
			proposed: g(func(x *Grant) { x.AccountID = model.NewID() }),
			want:     false,
		},
		{
			name: "a wider selector covers a narrower one",
			held: Grant{AccountID: a, Type: TypeDNSRecord, Action: "dns.write",
				Selector: &DNSSelector{Zone: "example.com", Names: "*.example.com", Types: []string{"A", "AAAA", "MX"}}},
			proposed: Grant{AccountID: a, Type: TypeDNSRecord, Action: "dns.write",
				Selector: &DNSSelector{Zone: "example.com", Names: "*.staging.example.com", Types: []string{"A"}}},
			want: true,
		},
		{
			name: "a selector does not cover a type it lacks",
			held: Grant{AccountID: a, Type: TypeDNSRecord, Action: "dns.write",
				Selector: &DNSSelector{Zone: "example.com", Names: "*.example.com", Types: []string{"A"}}},
			proposed: Grant{AccountID: a, Type: TypeDNSRecord, Action: "dns.write",
				Selector: &DNSSelector{Zone: "example.com", Names: "*.example.com", Types: []string{"A", "MX"}}},
			want: false,
		},
		{
			name: "a selector does not cover another zone",
			held: Grant{AccountID: a, Type: TypeDNSRecord, Action: "dns.write",
				Selector: &DNSSelector{Zone: "example.com", Names: "*.example.com", Types: []string{"A"}}},
			proposed: Grant{AccountID: a, Type: TypeDNSRecord, Action: "dns.write",
				Selector: &DNSSelector{Zone: "example.org", Names: "*.example.org", Types: []string{"A"}}},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Covers(tc.held, tc.proposed); got != tc.want {
				t.Fatalf("Covers = %v, want %v\n held: %s\n prop: %s", got, tc.want, tc.held, tc.proposed)
			}
		})
	}
}

// TestUnionCoverageIsRefused is the deliberate under-approximation, named so the
// refusal reads as intended rather than as a gap. A writer holding two load
// balancers cannot issue one grant covering the project containing both, even
// when those are the only two in it — because tomorrow they may not be.
func TestUnionCoverageIsRefused(t *testing.T) {
	a := model.NewID()
	p, lb1, lb2 := model.NewID(), model.NewID(), model.NewID()

	writer := Subject{AccountID: a, Grants: []Grant{
		{AccountID: a, Type: TypeLoadBalancer, ObjectID: id(lb1), Action: Wildcard, Inheritable: true},
		{AccountID: a, Type: TypeLoadBalancer, ObjectID: id(lb2), Action: Wildcard, Inheritable: true},
	}}
	proposed := Grant{AccountID: a, Type: TypeProject, ObjectID: id(p), Action: Wildcard, Inheritable: true}

	err := CheckSubset(writer, proposed)
	if !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("err = %v, want ErrNotPermitted — coverage must not be assembled from several held grants", err)
	}
}

// An unrestricted writer covers everything anchored to its own account, and
// nothing anchored elsewhere.
func TestUnrestrictedWriterCoversItsOwnAccountOnly(t *testing.T) {
	a, other := model.NewID(), model.NewID()
	writer := Subject{AccountID: a, Unrestricted: true}

	if err := CheckSubset(writer, Grant{AccountID: a, Type: TypeAny, Action: Wildcard}); err != nil {
		t.Fatalf("an unrestricted writer must be able to issue anything in its own account: %v", err)
	}
	if err := CheckSubset(writer, Grant{AccountID: other, Type: TypeAny, Action: Wildcard}); err == nil {
		t.Fatal("an unrestricted writer issued a grant into another account")
	}
}

// A writer cannot lift itself: issuing an unrestricted grant it does not hold is
// the escalation the whole rule exists to stop.
func TestUnrestrictedGrantCannotBeSelfIssued(t *testing.T) {
	a := model.NewID()
	writer := Subject{AccountID: a, Grants: []Grant{
		{AccountID: a, Type: TypeLoadBalancer, ObjectID: id(model.NewID()), Action: "inventory.write"},
	}}
	if err := CheckSubset(writer, Grant{AccountID: a, Type: TypeAny, Action: Wildcard}); err == nil {
		t.Fatal("a writer issued itself an unrestricted grant")
	}
}

func TestGrantValidation(t *testing.T) {
	a := model.NewID()
	oid := model.NewID()
	for _, tc := range []struct {
		name string
		g    Grant
		ok   bool
	}{
		{"a plain grant", Grant{AccountID: a, Type: TypeLoadBalancer, Action: "x"}, true},
		{"no account", Grant{Type: TypeLoadBalancer, Action: "x"}, false},
		{"no action", Grant{AccountID: a, Type: TypeLoadBalancer}, false},
		{"no type", Grant{AccountID: a, Action: "x"}, false},
		{"both an id and a selector", Grant{AccountID: a, Type: TypeDNSRecord, Action: "x", ObjectID: &oid,
			Selector: &DNSSelector{Zone: "example.com", Names: "*.example.com", Types: []string{"A"}}}, false},
		{"a selector on a type that defines none", Grant{AccountID: a, Type: TypeLoadBalancer, Action: "x",
			Selector: &DNSSelector{Zone: "example.com", Names: "*.example.com", Types: []string{"A"}}}, false},
		{"a wildcard type naming one object", Grant{AccountID: a, Type: TypeAny, Action: "x", ObjectID: &oid}, false},
		{"a selector with an interior wildcard", Grant{AccountID: a, Type: TypeDNSRecord, Action: "x",
			Selector: &DNSSelector{Zone: "example.com", Names: "web-*.example.com", Types: []string{"A"}}}, false},
	} {
		err := tc.g.Validate()
		if tc.ok && err != nil {
			t.Errorf("%s: %v, want nil", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: nil, want a refusal", tc.name)
		}
	}
}
