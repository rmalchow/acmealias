package adminctl

import (
	"testing"

	"gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"
)

// The grant form an operator types, over the shapes it has to survive.
//
// A malformed grant must be refused at parse time rather than stored and
// puzzled over later: a key that silently holds less than intended fails at
// three in the morning, which is when the person who typed it is least able to
// work out why.
func TestParseGrant(t *testing.T) {
	id := model.NewID()

	for _, tc := range []struct {
		name            string
		spec            string
		wantErr         bool
		wantType        string
		wantAction      string
		wantInheritable bool
		wantWildcard    bool
	}{
		{
			name: "a load balancer grant", spec: "load_balancer:" + id.String() + ":challenge.publish",
			wantType: "load_balancer", wantAction: "challenge.publish", wantInheritable: true,
		},
		{
			name:     "a project grant, which is the case the change exists for",
			spec:     "project:" + id.String() + ":challenge.publish",
			wantType: "project", wantAction: "challenge.publish", wantInheritable: true,
		},
		{
			name:     "a wildcard object is anchored to the account, not to everywhere",
			spec:     "load_balancer:*:challenge.publish",
			wantType: "load_balancer", wantAction: "challenge.publish", wantInheritable: true, wantWildcard: true,
		},
		{
			name:     "noinherit narrows it to the named object alone",
			spec:     "project:" + id.String() + ":inventory.view:noinherit",
			wantType: "project", wantAction: "inventory.view", wantInheritable: false,
		},
		{name: "too few parts", spec: "load_balancer:" + id.String(), wantErr: true},
		{name: "too many parts", spec: "a:b:c:noinherit:extra", wantErr: true},
		{name: "an empty part", spec: "load_balancer::challenge.publish", wantErr: true},
		{name: "a fourth part that is not noinherit", spec: "a:*:c:maybe", wantErr: true},
		{name: "an object that is not an identifier", spec: "load_balancer:seven:challenge.publish", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parseGrant(GrantSpec(tc.spec))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseGrant(%q) = %+v, want a refusal", tc.spec, g)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGrant(%q): %v", tc.spec, err)
			}
			if g.ObjectType != tc.wantType || g.Action != tc.wantAction {
				t.Errorf("got [%s, %s], want [%s, %s]", g.ObjectType, g.Action, tc.wantType, tc.wantAction)
			}
			if g.Inheritable != tc.wantInheritable {
				t.Errorf("inheritable = %v, want %v", g.Inheritable, tc.wantInheritable)
			}
			if tc.wantWildcard && g.ObjectID != nil {
				t.Errorf("object = %v, want a wildcard", g.ObjectID)
			}
			if !tc.wantWildcard && g.ObjectID == nil {
				t.Error("object is a wildcard, want the named identifier")
			}
		})
	}
}
