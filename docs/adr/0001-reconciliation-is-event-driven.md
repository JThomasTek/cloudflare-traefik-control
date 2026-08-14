# Reconciliation is event-driven; restart is the recovery mechanism

CTC reconciles only when something tells it to: an fsnotify write on the Traefik
config file, or a WAN IP check that finds a new address. There is no periodic
full reconcile. Startup runs one WAN IP reconcile and then one config
reconcile, both fatal on error, so a run that cannot establish its starting
position exits and the container's restart policy tries again.

## Considered options

**A WAN IP reconcile that triggers a config reconcile.** Attractive because it
makes the system self-healing: a config reconcile deferred for want of a WAN IP
would be retried the moment the address arrived, and startup ordering would
stop mattering. Rejected because it puts the config reconcile on both
goroutines at once. Two concurrent runs can each snapshot the state file, each
see the same router absent, and each call `Zone.Add` for it — which `zone.go`
documents as undefined. That is the class of bug the `Store` was introduced to
remove, reappearing one layer up.

**A slow ticker on the config path**, alongside fsnotify. Real drift
correction: it would also catch records deleted out from under CTC in the
Cloudflare dashboard. Rejected for now as a standing cost — periodic API
traffic, forever — against a problem that `--restart always` already solves.
Worth reopening if drift is observed in practice.

## Consequences

Recovery from a failed startup is a process restart, so CTC must be run under a
restart policy. This is why both initial reconciles call `log.Fatal` rather
than logging and continuing: a container that exits loudly is better than one
that runs managing nothing.

Because nothing re-triggers a config reconcile except a write to the config
file, a reconcile that silently does less than it should is not self-correcting.
`CompareStateToConfig` therefore defers adds when no WAN IP is known rather than
creating records that point at nothing — a record created wrong would be
recorded in the state file and never revisited.
