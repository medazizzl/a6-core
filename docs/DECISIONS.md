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
