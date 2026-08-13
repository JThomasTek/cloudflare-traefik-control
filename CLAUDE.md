# CLAUDE.md

Guidance for AI assistants working in this repository.

## Overview

**Cloudflare Traefik Control (CTC)** is a small Go daemon that reads a Traefik
dynamic-config file, extracts the `Host(...)` rules from its HTTP routers, and
keeps matching DNS `A` records in a Cloudflare zone in sync. It also behaves as
a dynamic DNS client: it periodically checks the host's WAN IP and updates the
records it manages whenever the IP changes.

The program runs as a long-lived process (typically in Docker) and reacts to two
event sources:

1. **Traefik config file changes** — watched via `fsnotify`. New routers create
   DNS records; removed routers delete them.
2. **WAN IP changes** — polled every 60 seconds against `ipv4.icanhazip.com`.

It only ever touches records it created. Ownership is tracked by writing a
comment (`Managed by ctc: <routerName>`) on each Cloudflare DNS record and by
keeping a local state file.

## Module / repository facts

- Go module path: `github.com/JThomasTek/traefik-config-to-cloudflare` (note:
  this differs from the repo name `cloudflare-traefik-control`).
- Go version: `go 1.25.0` (toolchain `go1.25.11`).
- Published images: `ghcr.io/jthomastek/cloudflare-traefik-control` and
  `jthomastek/cloudflare-traefik-control` (Docker Hub).
- License: MIT.

## Layout

```
cmd/ctc/main.go        Entry point: config from env, builds Zone + Reconciler
internal/              All application logic (single package `internal`)
  zone.go              Zone interface: the seam between reconcile and DNS
  zone_cloudflare.go   Zone adapter backed by the Cloudflare API
  ownership.go         Ownership comment: render + parse
  reconcile.go         Reconciler: diffs state against config and WAN IP
  traefik.go           Traefik config parsing + fsnotify file watcher
  wan_ip.go            WAN IP lookup + polling loop
  state.go             State file (read/write)
  *_test.go            Unit tests; zone_fake_test.go holds the in-memory Zone
Dockerfile             Multi-stage build (golang:alpine -> alpine:latest)
.github/workflows/     CI: build-image (PR/push), publish-image (release)
.vscode/launch.json    Local debug launch config with sample env vars
```

Unit tests live alongside the source in `internal/*_test.go` (white-box, package
`internal`). They cover the pure/parsing logic (`cleanRule`, the ownership
comment round trip, Traefik config parsing), state file round-tripping, the WAN
IP fetch (via `httptest`), the reconciliation logic in `CompareStateToConfig` /
`CompareStateToWanIP`, and the Cloudflare adapter itself (also via `httptest` —
assign `api.BaseURL` after constructing the client).

Reconcile tests run against `fakeZone` (`zone_fake_test.go`), the in-memory
`Zone` adapter, so no Cloudflare access is needed. Add a new dependency by
putting it behind the `Zone` interface rather than by introducing a package-level
function var. Run with `go test -race ./...`.

## How it works (control flow)

`main()` in `cmd/ctc/main.go`:
1. Sets the zerolog global level from `LOG_LEVEL` (`trace`/`debug`/default `info`).
2. Reads env vars (see below) and compiles the host-ignore regex.
3. Builds the Cloudflare-backed `Zone` (`NewCloudflareZone`). Fatal if
   `CLOUDFLARE_API_TOKEN` is unset.
4. Builds the `Reconciler` (`NewReconciler`) over that zone, the config file
   path and the compiled ignore regex, then runs an initial WAN IP check and an
   initial config reconciliation.
5. Starts two goroutines: `reconciler.TraefikConfigWatcher` (fsnotify) and
   `reconciler.WanIPCheck(ctx, 60)`.
6. Adds the config file's **directory** to the fsnotify watcher, then blocks
   forever on `<-make(chan struct{})`.

Reconciliation lives in `internal/reconcile.go`, on the `Reconciler`:
- `CompareStateToConfig` diffs the parsed Traefik routers against the state file:
  added routers (not matching the ignore regex) are created via `Zone.Add` and
  recorded; removed routers go through `Zone.Remove` and leave the state.
- `CompareStateToWanIP` calls `Zone.SetIP` when the WAN IP changes.
- `cleanRule` extracts the hostname from a Traefik rule by parsing the substring
  between `` Host(` `` and `` `) ``.

Both only touch the state file once the zone confirms the change, so a failed
call never leaves state claiming something that did not happen.

### State file

- Default location: `/etc/ctc/state.yml` (constants `stateFolder` / `stateFile`
  in `internal/state.go`). The directory is created (mode `0744`) if missing.
- A `sync.Mutex` (`mu`) guards all reads/writes of the state file.
- Persists `WanIP` and a `Routers` map (router name -> rule).
- Note: `TRAEFIK_STATE_FILE` appears in `.vscode/launch.json` but is **not** read
  by the code today — the state path is hardcoded.

### Record ownership

- Every record CTC creates carries the comment `Managed by ctc: <routerName>`.
  Rendering and parsing both live in `internal/ownership.go`; the prefix is the
  `ownershipPrefix` constant and nothing else should hardcode it. Changing it
  orphans every record already carrying the old one.
- The ownership comment does not cross the `Zone` seam — the adapter renders it
  on create and parses it when listing, so record IDs, comments, TTL and the
  proxy flag all stay inside `zone_cloudflare.go`.
- Records are created as `Type: "A"`, `TTL: 1` (automatic), `Proxied: true`.
- `Zone.SetIP` updates **every** record carrying the ownership comment, including
  orphans whose router has left the state file — a record still carrying our mark
  is still ours, and skipping it would strand it on a stale address.
- `Zone.Remove` is idempotent: removing a router that owns no record is not an
  error, which is what makes reconciliation safe to retry after a crash.

## Configuration (environment variables)

| Variable | Default | Required | Notes |
| --- | --- | --- | --- |
| `CLOUDFLARE_API_TOKEN` | — | Yes | API token auth (key/email auth exists in code but is unused). |
| `CLOUDFLARE_ZONE_ID` | — | Yes | Target Cloudflare zone. |
| `TRAEFIK_CONFIG_FILE` | `/etc/traefik/config.yaml` (README) / `/etc/traefik/config.yml` (code default) | Yes | Path to the Traefik dynamic config file. |
| `TRAEFIK_HOST_IGNORE_REGEX` | `^$` | No | Hostnames matching this regex are skipped (e.g. local-only DNS). |
| `LOG_LEVEL` | `info` | No | `trace`, `debug`, or default. |

> The README documents the default config path as `config.yaml`, while the code
> default in `main.go` is `config.yml`. Keep both in mind; prefer setting
> `TRAEFIK_CONFIG_FILE` explicitly.

## Build, run, and develop

This environment may not have a Go toolchain or Docker preinstalled; verify
before assuming commands will run.

```bash
# Build the binary
go build -o ctc ./cmd/ctc/main.go

# Vet / format (no linter config is committed; use the stdlib tooling)
go vet ./...
gofmt -l .

# Tidy modules
go mod tidy

# Run locally (set env vars first)
LOG_LEVEL=debug \
TRAEFIK_CONFIG_FILE=./config.yml \
CLOUDFLARE_API_TOKEN=... CLOUDFLARE_ZONE_ID=... \
go run ./cmd/ctc/main.go
```

Docker:

```bash
docker build -t ctc .
docker run -d --restart always \
  -e CLOUDFLARE_API_TOKEN=... -e CLOUDFLARE_ZONE_ID=... \
  -v /path/to/ctc:/etc/ctc -v /etc/traefik:/etc/traefik \
  ghcr.io/jthomastek/cloudflare-traefik-control:latest
```

For local debugging, `.vscode/launch.json` provides a "Launch Test Package"
configuration with sample env vars (fill in the empty token/zone fields).

`config.y*ml` and `state.y*ml` are gitignored — never commit real config or
state files.

## CI / release

- `.github/workflows/build-image.yaml`: first runs a `test` job
  (`go vet`, `go test -race`, `govulncheck`), then builds the multi-arch image
  (`linux/amd64,linux/arm64`) on pushes/PRs to `main` and `develop`. The image
  build `needs: test`, so it only runs when tests pass. Does **not** push
  (`push: false`) — it's a build/test verification gate.
- `.github/workflows/publish-image.yaml`: on a published GitHub **release**,
  builds and pushes multi-arch images to GHCR and Docker Hub with semver tags.
- Versioning is driven by GitHub releases (semver tags); there is no separate
  version constant in the code.

## Conventions

- **Logging:** use `github.com/rs/zerolog/log`. Pattern is
  `log.<Level>().Err(err).Msg("...")`, with structured fields via `.Str(k, v)`.
  Use `log.Fatal()` only for unrecoverable startup errors in `main`; in
  background/reconciliation code, log errors with `log.Error()` and continue.
- **Package boundary:** keep application logic in `internal`; `cmd/ctc/main.go`
  stays a thin wiring layer (env parsing + goroutine startup).
- **The Zone seam:** reconciliation talks to DNS only through the `Zone`
  interface, in CTC's own terms (routers, hosts, an IP). Provider types, record
  identifiers and the ownership comment must not appear in its signatures. New
  DNS behaviour belongs behind `Zone`, with the fake updated to match.
- **Domain vocabulary:** `CONTEXT.md` at the repo root is the glossary. Use its
  terms in names, comments and commit messages, and add to it when a new concept
  earns one.
- **Config:** all runtime configuration comes from environment variables read in
  `main.go`. Add new options there and document them in both this file and the
  README.
- **YAML:** parsing uses `gopkg.in/yaml.v3`. The `TraefikConfig` struct is
  intentionally minimal (only `http.routers[*].rule`); extend the struct if you
  need more of the Traefik schema.
- **Concurrency:** the config watcher debounces writes (100ms timer per path);
  state file access is mutex-guarded. Preserve these guards when touching
  `traefik.go` / `state.go`.
- Match the existing style; gofmt everything.

## Roadmap / known gaps (from `main.go` TODO + code)

- Docker label support (read hosts from container labels, not just config file).
- Multiple-domain / multi-zone support.
- Ability to disable WAN IP updates.
- Cloudflare API key + email auth. Only token auth exists; the unreachable
  key/email initializer was removed rather than left dangling.
- `TRAEFIK_STATE_FILE` env var is referenced in tooling but not honored by code.
- State file access is still package-level (`getState` / `writeState` over
  `stateFolder` / `stateFile` globals), and the mutex guards only the individual
  file reads and writes rather than the whole read-modify-write cycle, so the
  two reconcile goroutines can lose each other's updates.

## Working agreements for assistants

- Work on a task-specific `claude/<short-slug>` branch cut from `main`. Commit
  there; do not push to `main`/`develop` without explicit permission, and do not
  open PRs unless asked.
- When you change behavior or configuration, update **both** this file and
  `README.md` so the two don't drift.
- Unit tests live in `internal/*_test.go`; keep them green (`go test -race ./...`)
  and add coverage when you add functionality. The `build-image` workflow runs
  `go vet`, `go test -race`, and `govulncheck` as a gate before the image build.

## Agent skills

### Issue tracker

Issues live as GitHub issues in `JThomasTek/cloudflare-traefik-control`, managed
via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical roles, each label string equal to its name (`needs-triage`,
`needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See
`docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root, both created
lazily. `CONTEXT.md` exists; there are no ADRs yet. See `docs/agents/domain.md`.
