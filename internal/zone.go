package internal

import "context"

// Zone is the seam between reconciliation and DNS hosting. It is expressed in
// CTC's own terms — routers, hosts and an IP — so that record identifiers,
// ownership comments, TTLs and proxy settings stay behind it.
//
// Implementations must honour the following, which callers rely on:
//
//   - Add creates an address record for host pointing at ip, claimed for
//     router. The reconciler only calls Add for routers absent from the state
//     file, so the behaviour of adding a router twice is not defined.
//
//   - SetIP points every record the implementation owns at ip. Records owned by
//     CTC but no longer named in the state file are updated too: a record still
//     carrying our mark is still ours, and skipping it would strand it on a
//     stale address forever. Failures are aggregated rather than short
//     circuited, so one unwritable record cannot block the others. Cost is one
//     listing plus one update per owned record.
//
//   - Remove deletes the record owned by router, and is idempotent: removing a
//     router that owns no record is not an error. That is what makes the
//     reconciler safe to retry after a crash part-way through a delete.
//
// Every method takes a context so callers can bound or cancel the work.
type Zone interface {
	Add(ctx context.Context, router, host, ip string) error
	SetIP(ctx context.Context, ip string) error
	Remove(ctx context.Context, router string) error
}
