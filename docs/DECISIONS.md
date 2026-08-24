# A6 Core — Decision Log

Durable record of design decisions and their reasoning. Chat history dies;
this file doesn't. When a future session asks "why is it like this?", the
answer belongs here.

## Standing workflow (born from real incidents)

- Design → review → execute exactly what's given → report real output → next step.
- Read a file's actual contents before modifying it. Never assume.
- Unrelated files are never touched; "helpful" fixes are reported and held for review.
- Passing tests prove only that the tests that exist still pass — never that a
  stage is architecturally correct or complete.
- Naming: Go module/import path is `a6core` (no hyphen); repo/folder is `a6-core`
  (with hyphen). Both intentional, do not "fix" either.

## Stage 8–10 (summary)

- Mode persisted in `state.json` with empty→"tv" migration (A6 Core owns mode).
- `/v1/modes` request field is `"target"` (not `"mode"`), success is 202 (not 200).
- Icons: strict UUID validation before any path construction; content sniffing
  overrides claimed types; PNG/JPEG uploads only; 512KB cap; atomic writes.
- Favicon fetch: SSRF guard checks EVERY resolved IP on EVERY connection,
  including redirects; soft-failure design never blocks shortcut creation.
- Classic `.ico` favicons are converted to PNG at the fetch boundary; manual
  upload strictness unchanged (fetched `.ico` ≠ accepted upload format).
- Response shapes follow the frozen spec byte-for-byte; two incidents of silent
  drift (Stage 9 route disconnection, Stage 10 `internal/api` detour) are why.

## Stage 11 — App/session management & retro launch

1. **One launch implementation, two doors.** `/v1/apps/launch` and
   `/v1/retro/launch` share `apps.Manager.Launch` AND one error-mapping function
   (`writeLaunchError`). No parallel implementations that could drift.
2. **Session state is memory-only.** Unlike Mode, an app session is a claim
   about an external process; persisting it would misstate reality after any
   unclean exit. Spec §12 philosophy: reconstruct from ground truth. The
   ground-truth query seam arrives with Stage 15.
3. **Implicit replace, with three pinned sub-behaviors:** (a) close on idle is
   idempotent 202 and never invokes the executor; (b) replacement is strictly
   close-old-then-launch-new, sequential; (c) if the old closed but the new
   launch failed, current becomes nil — fail-closed, no resurrection.
4. **PS2 absent entirely** from the v1 console list — not listed as unavailable.
   Absence is honest; a permanent false row would promise untested architecture.
5. **Console allowlist** (`nes,snes,genesis,ps1,n64`) lives in exactly one
   function (`retro.ValidateConsole`); both routes validate through it. Display
   names frozen: NES, SNES, Genesis, PS1, N64. Unknown console → 422 everywhere,
   including the games-listing route (never a silent empty array).
6. **ROM listing rules (frozen):** flat scan of `{DataDir}/roms/<console>/`;
   subdirectories skipped, never recursed; dotfiles skipped (filesystem litter,
   not content gatekeeping — different category from format filtering);
   NO extension filtering in v1 (nobody has verified which formats RetroArch
   cores accept on this hardware; pretending otherwise would be unearned
   architecture). Missing directory = zero games, not an error; other read
   errors propagate loudly.
7. **game_id/title derivation:** `game_id` is the literal filename byte-for-byte
   (case-sensitive, no normalization); `title` is that filename minus its last
   extension (only when the stem is non-empty). Existence is checked by
   re-scanning and comparing — a client-supplied game_id NEVER becomes a path.
8. **BlockingResource seam declared, not wired.** `apps.Manager.Blocking()` is
   implemented now; registering it with modes/system is a separate reviewed
   change in Stage 12/15. Resource IDs are human-readable:
   `browser`, `shortcut:<shortcut_id>`, `retro:<console>/<game_id>` — these may
   surface in 409 responses someday.
9. **GET /v1/apps/current response extended additively:** `console`/`game_id`
   appear (omitempty) exactly when `type=="retro"`. The spec example showed a
   shortcut session; type-specific optional fields are established precedent.
10. **apps.Manager has its own mutex**, deliberately separate from
    `state.Store`'s — two locks for two stores, since session state deliberately
    lives outside `state.json`. Race-detector verified (`go test -race`).
    This required cgo: see environment notes.

## Environment notes (dev machine)

- `gcc` is installed on the Acer solely because `go test -race` requires cgo.
  It is a BUILD-TIME tool only — the shipped binary has zero runtime dependency
  on it.
- First pacman mirrorlist entry is `geo.mirror.pkgbuild.com` (GeoIP CDN),
  added after a slow-mirror download failure. Original preserved at
  `/etc/pacman.d/mirrorlist.bak-opencode`.
- Execution model during development: opencode agent drives the Acer over
  passwordless SSH from the Windows desktop; file transfers use scp from local
  byte-exact copies (heredoc-over-shell pasting caused the original main.go
  truncation incident).

## Stage 12 — Server Registry & Minecraft Lifecycle

1. **No creation endpoint — hardcoded catalog.** The frozen spec defines
   `GET /v1/servers`, `GET /v1/servers/{id}`, `POST .../start`, `POST .../stop`
   — no create endpoint. Building CRUD with no population path would be
   architecture theater. Server definitions live as a fixed list in
   `internal/servers` (mirrors `internal/retro`'s console list). Currently
   one entry: Minecraft (`minecraft`, type `minecraft`, "Survival World",
   port 25565). Terraria/Factorio become one-line additions when actually
   tested on this hardware.

2. **Runtime status in-memory only.** Same reasoning as Stage 11's app
   sessions: a persisted claim about a live external process misrepresents
   reality after any unclean restart. Spec §12's recovery philosophy
   applies identically here.

3. **`Stop()` is async; `Start()` stays sync.** The frozen spec's
   graceful-shutdown sequence (§10) is explicitly multi-phase with real
   wait times — broadcast, 30s grace, send stop, poll up to 60s,
   escalate only as last resort. That can't be a blocking HTTP call.
   `Stop()` marks `"stopping"`, returns 202, runs sequence in background
   goroutine. Durations are constructor-injectable (tests use ms, prod
   uses spec defaults). `Start()` has no phased algorithm in the spec,
   stays synchronous.

4. **`force` on stop means "re-trigger sequence", not "kill".** On
   `/v1/servers/{id}/stop`, `force` skips the `ErrStopInProgress` guard
   and re-triggers the sequence — the broadcast/grace/poll/escalate
   steps run identically either way. This differs from modes/system
   where `force` gates proceeding past a blocking resource. Here the
   resource IS the thing being stopped, so `force` has a narrower
   meaning. `ErrStopInProgress` → 409 `stop_in_progress` (same pattern
   as `busy_resource` everywhere else).

5. **This is the stage that wires `apps.Manager.Blocking()` and
   `servers.Manager.Blocking()` into something real.** Since Stage 8/9,
   `modes.NewManager` and `system.NewManager` received `nil` checkers.
   Now `main.go` constructs `appMgr` and `srvMgr` first, builds a
   `combinedChecker` from both, and passes it to both managers. The
   checker logic is real and tested — honest caveat: nothing sets a
   player count above zero until Stage 15 provides RCON, so it returns
   empty in practice today. The logic is real; the data source is still
   a stub.

6. **Lifecycle status values are explicit:** `stopped` (initial),
   `starting` (transient), `running` (after Start succeeds),
   `stopping` (after Stop returns, until grace+poll+escalate
   completes). Failed Start leaves status `stopped` (honest). Failed
   graceful stop leaves status `stopping` (honest ambiguity — never
   falsely claims stopped).

6. **Stop sequence implements spec §10 steps 2-7 exactly:**
   2. Broadcast in-game warning (soft-failure — executor failure
      doesn't abort shutdown)
   3. Wait grace period (30s spec default)
   4. Send graceful stop command
   5. Poll `IsStopped` up to maxWait (60s spec default)
   6. Escalate (second Stop call) ONLY if polling timed out — leave
      status `stopping` (honest ambiguity, never falsely `stopped`)
   7. Mark `stopped` on confirmed termination — clear players

7. **In-progress guard prevents concurrent Stop sequences.**
   `ErrStopInProgress` (409) if Stop called while one is in flight.
   HTTP layer's `force=true` calls `ForceRestartStopSequence` which
   bypasses the guard and starts a fresh sequence.

8. **Concurrency safety:** `Manager` has its own `sync.RWMutex`
   (separate from `state.Store`), background goroutine in `Stop`
   properly cleans up `stopRunning` flag via `defer`. Race detector
   verified (`go test -race`).

Environment notes:
- Real 30s grace period runs in production (test suite uses
  injectable ms durations via constructor). Live verification confirmed
  `stopping` → 32s wait → `stopped` on the real Acer.

## Stage 13 — WebSocket Event Stream

1. **First non-stdlib dependency: github.com/coder/websocket.** WebSocket
   framing (RFC 6455) is hard to hand-roll correctly; this library is
   small, actively maintained, has no transitive deps, and uses
   context.Context patterns. Precedent: gcc for -race is the
   same category — a justified exception to the stdlib-only rule.

2. **Heartbeat is application-level JSON ({event:" ping\} /
 {event:\pong\}), not transport ping/pong.** Keeps heartbeat logic
 testable without the WS library. Spec-explicit.

3. **One envelope for all messages:** {event:\...\, data:{...}},
 data omitted for ping/pong. Matches spec's connected example.

4. **Slow client never blocks the hub.** Per-client buffered channel
 (16 messages) with non-blocking enqueue — dropping one tick for
 a slow client is fine; a frozen broadcast loop is not.

5. **Auth extraction — single shared implementation.** ValidateDeviceKey
 extracted from auth.middleware (Stage 7) and now called by both
 REST middleware and WebSocket handshake. Auth tests (4/4) pass
 unchanged — zero behavior change.

6. **Events published from HTTP handlers, not business logic.** Each
 handler publishes on success; business logic packages (modes,
 apps, servers, auth) remain completely unaware of events.
 No new cross-package dependencies.

7. **device.paired/device.revoked broadcast to all clients.**
 Security-wise fine: only paired devices can ever authenticate to
 the WebSocket, so an unpaired device can never receive these.
 Spec's own intent is notification to already-paired peers.

8. **status.tick runs on its own 5s ticker in Hub**, independent of
 heartbeat. StatusProvider closure in main.go closes over
 modeMgr + telemetry.Sampler — telemetry never imported by
 internal/events (stubbed seam pattern).

9. **network field in StatusTick explicitly deferred** — needs a
 small network reader in telemetry; deferred to a follow-up Wave 5
 before Stage 13 called fully complete.

10. **Background lifecycle: three goroutines (Hub.Run,
 Hub.RunStatusTicks, telemetry.Sampler.Run) share ONE
 context.Context (bgCtx), cancelled simultaneously with
 HTTP server's graceful shutdown. No leaked goroutines on Ctrl+C.

11. **servers.Manager.SetOnChange callback** — internal/servers stays
 unaware of events; main.go wires hub.Publish through the
 callback, keeping the event import out of internal/servers.

Environment notes:
- github.com/coder/websocket v1.8.15 added to go.mod (first
 non-stdlib runtime dep).
- gcc remains build-time only (for -race).
