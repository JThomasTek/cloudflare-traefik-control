# Cloudflare Traefik Control

CTC keeps a DNS zone in step with the routes a Traefik instance is serving, and
keeps the addresses in that zone pointing at the host's current WAN IP. It is
the one thing standing between "Traefik knows about this hostname" and "the
internet can resolve it".

## Language

### What CTC reads

**Router**:
A named entry in the Traefik config that CTC treats as a request for a
hostname to resolve. The name is CTC's permanent handle on everything it
creates on that router's behalf.
_Avoid_: route, service, entry

**Host**:
The hostname a router asks for. One host per router.
_Avoid_: subdomain, domain, FQDN, rule

**Ignored host**:
A host CTC deliberately leaves alone, because it is served somewhere the
public internet is not meant to reach it.
_Avoid_: excluded host, filtered host, blocked host

**WAN IP**:
The address the internet currently reaches this host on. It changes without
warning, and every managed record is expected to follow it.
_Avoid_: public IP, external IP, my IP

### What CTC writes

**Zone**:
The DNS zone CTC is pointed at. It holds records CTC owns alongside records it
must never touch.
_Avoid_: domain, DNS provider, Cloudflare

**Managed record**:
A DNS record CTC created and is therefore allowed to change or delete. Every
managed record belongs to exactly one router.
_Avoid_: DNS entry, our record, A record

**Ownership comment**:
The mark CTC leaves on a record to claim it and to name the router it belongs
to. A record without the mark is somebody else's and is left untouched.
_Avoid_: tag, label, annotation, marker

**Orphaned record**:
A managed record whose router is no longer named in the state file. It still
carries the ownership comment, so it is still CTC's to maintain.
_Avoid_: stale record, dangling record

### What CTC decides

**Reconciliation**:
Bringing the zone into agreement with what the Traefik config and the WAN IP
now say. It runs whenever either of those changes, and is expected to be safe
to repeat.
_Avoid_: sync, refresh, update, diff

**State file**:
CTC's record of what it believes it has already done — which routers it has
created records for, and the WAN IP those records carry. It is what makes a
restart pick up where the last run left off rather than starting from nothing.
_Avoid_: cache, database, store
