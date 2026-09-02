// Package commissionerhq implements the read-only Commissioner HQ
// federation. Each league instance exposes a small, PII-free Summary
// (counts, state, and operator guidance only) at a fixed HTTP path, and a
// Service fetches its own local summary plus its configured peers' summaries
// so a commissioner can see every league's status from one dashboard.
//
// The federation never carries identities, invites, boards, sessions,
// cookies, CSRF tokens, or raw errors across instances, and it never issues
// mutations: a peer read only calls the local SummarySource or an HTTP GET
// against a peer's SummaryPath.
package commissionerhq
