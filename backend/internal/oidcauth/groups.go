package oidcauth

import "strings"

// Groups reads the group membership out of an ID token's claims.
//
// The claim name is configuration (ACME_OIDC_GROUP_CLAIM, default "groups")
// because providers disagree: Keycloak needs a Group Membership mapper on the
// client, Authentik supplies the same list under the "groups" scope. The
// optional prefix (ACME_OIDC_GROUP_PREFIX) is stripped from each value, so a
// Keycloak full group path like "/acme/admins" reduces to "admins" with the
// prefix set to "/acme/".
//
// The list is what the session records, verbatim; deriving roles from it is
// internal/authz's job.
//
// A missing claim, a claim of the wrong shape, or one whose entries are not
// strings yields no groups — which means no roles, and a sign-in that is
// authenticated but permitted nothing. That is the correct failure: it is
// visible to the user and cannot grant anything.
func Groups(claims map[string]any, claimName, prefix string) []string {
	if claimName == "" {
		claimName = "groups"
	}
	raw, ok := claims[claimName]
	if !ok {
		return nil
	}

	var values []string
	switch v := raw.(type) {
	case string: // some providers emit a single group as a bare string
		values = []string{v}
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				values = append(values, s)
			}
		}
	default:
		return nil
	}

	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		g := strings.TrimSpace(strings.TrimPrefix(v, prefix))
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	return out
}
