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

## Stage 16 — actually implemented (previously design-only)

Stage 16 had existed only as a design document for a while — a `grep -rn
IsPrimary` on the real repo turned up nothing at all. Implemented for real:
`Device.IsPrimary`, migration for pre-Stage-16 state.json (auto-promotes the
earliest-paired device if none is marked primary), first-ever-paired device
becomes primary automatically, `RequirePrimary` middleware, `POST
/v1/devices/{id}/promote` (additive only, no demotion mechanism — same
"last device" lockout risk as Stage 7, deferred as its own design pass).
All 8 real verification criteria (migration, first-pair, primary can act,
guest rejected, guest unaffected elsewhere, a raw manual request from a
guest is rejected server-side, survives restart, zero regressions) proven
with real `-race -count=10` runs against the actual Acer, plus one live
guest-rejection test against the real running server.

Also discovered live: the real GitHub repo was missing `internal/dbusctl`
entirely (referenced by main.go, never actually committed despite the
Stage 15 commit message claiming otherwise) and was several stages behind
the Acer's real local code. Real catch-up commit pushed before any of the
above was built, so Stage 16 was built against actual current code, not a
stale snapshot.

## Stage 18 — Real Minecraft executor (Bedrock, not Java)

Switched from Java/PaperMC to **Bedrock Dedicated Server** partway through
implementation — Paper was fully installed and manually proven working
first, then abandoned once it turned out Bedrock was what was actually
wanted. Installed via the `minecraft-bedrock-server` AUR package (verified
as real and actively maintained before use, not just trusted from an AI's
suggestion) rather than a hand-rolled install script, since it comes with
a proper systemd unit and dedicated service user out of the box.

Real executor wiring: `internal/dbusctl` extended with a narrow, allowlisted
systemd unit manager (`StartUnit`/`StopUnit`/`UnitActiveState` for
`minecraft-bedrock-server.service` only, not general unit control) — never
shells out to `systemctl`, per the standing "no arbitrary shell exec" rule.

Real incident: after wiring this up, real Start/Stop calls failed with
"Permission denied" despite a correct, narrowly-scoped polkit rule. Root
cause was in the *existing* `49-a6core.rules` (Stage 15), which ended with
an unconditional `return polkit.Result.NO` for anything outside its own
three actions — since polkit rule files are evaluated in order and the
first definitive answer wins, this silently blocked every later rule file
from ever being reached, for months, without anyone noticing until a
second rule file actually needed to run. Fixed by returning nothing
(not NO) for out-of-scope actions. Worth remembering: a "deny by default"
rule that returns an explicit NO instead of staying silent will block
every rule file loaded after it, not just requests it's actually meant to
gate.

Also found and fixed live: `NewManager` always initializes every server's
in-memory status to `StatusStopped`, with no reconciliation against
reality. After several a6core restarts during this stage's deployment, the
real Bedrock service had been running for 14+ hours while the app kept
reporting "stopped." Fixed with `SyncInitialStatus`, called once at
startup via a real `UnitActiveState` query — deliberately not a general
sync mechanism, ordinary transitions still go through Start/Stop only.

Real player-count stats: Bedrock has no RCON and no GameSpy4/UT3 query
protocol (Java's `enable-query` has no Bedrock equivalent at all — verified
against the actual protocol docs, not assumed). The only real status
mechanism Bedrock implements is the same RakNet Unconnected Ping/Pong every
Bedrock client sends to populate its own server list. `internal/bedrockping`
implements this for real (verified byte layout, tested against a real fake
UDP server, not just unit-tested against a hardcoded byte slice) and a
10s background poller in main.go keeps `Players` current — failures clear
it back to `nil` rather than showing a stale or fabricated count.

## Reboot/Shutdown permission redesign (post-Stage-16)

Stage 16's original design gated both `/v1/system/reboot` and
`/v1/system/shutdown` to primary devices. Deliberately changed:
**Reboot is now open to any paired device** (guest included); **Shutdown
remains primary-only**. Real product decision, not a bug — a guest
rebooting the appliance is low-stakes and occasionally useful; a guest
shutting it down fully is not.

A "fake Shutdown for guests that's actually a screen-sleep action" was
proposed and explicitly NOT built. Reasoning worth preserving: there is no
real shell/display layer yet to react to a sleep request, and real OS
suspend was already ruled out on this hardware (Stage 15 — left the
machine unreachable for 90+ minutes in testing). Building a button that
appears to do something while doing nothing visible was rejected outright,
and building event-publishing infrastructure with no real consumer yet was
also rejected — "define now if useful, implement when there's a real
consumer." The likely future contract, NOT implemented: a
`system.sleep_requested` event published via the existing Hub (same
pattern as `server.status_changed`), consumed by the shell once it exists,
which owns real display blanking per the original Stage 17 design (HDMI
auto-blanking is shell-owned, not a separate systemd/udev service). Define
the real behavior (screen blank vs. shell-level sleep vs. something else)
together with the shell, not in advance of it.
