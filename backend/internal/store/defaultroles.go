package store

import "gitlab-mcww-admin.versioncontrol.io/tools/acmeservice/internal/model"

// The role set a new team account starts with (design C10, task 2.5).
//
// These four names used to be a GLOBAL vocabulary: constants in internal/authz,
// a permission matrix keyed by them, and four environment variables saying which
// identity-provider group granted each one. All of that is gone. What survives
// is this table — the same four names carrying the same authority, written as
// rows into one account, where the account's owner can edit or delete them.
//
// The grants below are not a fresh design. They are transcribed from the
// migrations that first reproduced the pre-grant matrix as data, under
// constraint 1 ("the internal edition's observable behaviour must not change").
// That constraint is discharged rather than violated — see the note in
// openspec/changes/tenancy-and-permissions — and this is what it bought: the
// authority each name confers, written down once, verified against the seeded
// rows by TestIntegrationDefaultRolesAreTheOldMatrix.
//
// Two things are easy to get wrong here and are therefore stated:
//
//   - ADMIN has no grants at all. Its authority is the unrestricted flag, which
//     cannot be edited away one row at a time — that is what keeps "there is
//     always a way back into this account" true.
//   - CONFIG_ADMIN and KEY_ROTATION hold challenge.publish and challenge.withdraw
//     even though no console screen asks for them. They mint API keys, the subset
//     rule (P10) says a subject may grant only what it holds, and a role that can
//     create keys but holds no challenge grant can create only useless ones. That
//     omission shipped once, and cost a 403 on key creation for exactly the two
//     roles whose purpose is creating keys.
//
// Every grant is [*, *, action] and inheritable: a role-wide permission means
// "every object of this account", and a wildcard type with a wildcard identifier
// anchored to the account is exactly that.
func DefaultRoles() []model.Role {
	grants := func(actions ...string) []model.RoleGrant {
		out := make([]model.RoleGrant, 0, len(actions))
		for _, a := range actions {
			out = append(out, model.RoleGrant{ObjectType: "*", Action: a, Inheritable: true})
		}
		return out
	}
	return []model.Role{
		{
			Name:         "ADMIN",
			Description:  "Every action on every object in this account",
			Unrestricted: true,
		},
		{
			Name:        "CONFIG_ADMIN",
			Description: "Manage inventory, keys and DNS plans",
			Grants: grants(
				"inventory.write", "key.write", "key.secret.view",
				"dns.plan", "inventory.view",
				"challenge.publish", "challenge.withdraw",
			),
		},
		{
			Name:        "KEY_ROTATION",
			Description: "Manage API keys, and read what is needed to use them",
			Grants: grants(
				"key.write", "key.secret.view", "dns.plan", "inventory.view",
				"challenge.publish", "challenge.withdraw",
			),
		},
		{
			Name:        "READ_ONLY",
			Description: "Read inventory, DNS plans, verification results and the audit log",
			Grants:      grants("dns.plan", "inventory.view"),
		},
	}
}
