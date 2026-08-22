// Package adminctl implements the acmeservice command-line administration
// verbs (design D-cli): projects, load balancers, nodes, domains and API keys,
// operated directly against the datastore with no server process running.
// It exists for bootstrap and emergencies — before anyone has console
// access, and when the console is down.
//
// Every mutation goes through internal/inventory, the same service layer the
// HTTP admin API uses, so validation cannot diverge between the console and
// the CLI. Each function here does the inventory call, prints an
// operator-facing result to the given io.Writer, and appends one audit entry
// attributing the change to a CLI actor (the OS user and host).
package adminctl
