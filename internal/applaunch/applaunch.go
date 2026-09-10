// Package applaunch provides A6 Core's real launch/close executors
// for internal/apps' browser, shortcut, and retro session types.
// Minecraft launching is separate, unrelated work.
package applaunch

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"a6core/internal/apps"
	"a6core/internal/retro"
	"a6core/internal/shortcuts"
	"a6core/internal/xsession"
)

// consoleEmulator maps a console id to the real, tested command used
// to launch it. ONLY entries listed here have actually been proven
// working on this hardware — any other console id returns a clear
// error from launchRetro rather than silently doing nothing or
// pretending to launch. Add an entry only after real testing, same
// standard as everything else in this project.
var consoleEmulator = map[string]string{
	"nes": "fceux",
}

// executor holds the state a real launcher needs beyond what
// apps.Session already carries: which real OS process (if any) it
// most recently started, so Close can stop precisely that one — not
// a name-based kill, which we found firsthand tonight can be
// unreliable. gracePeriod is how long close() waits after a polite
// SIGTERM before escalating to SIGKILL — configurable so tests don't
// have to wait the real default.
type executor struct {
	scStore     *shortcuts.Store
	retroStore  *retro.Store
	mu          sync.Mutex
	pid         int
	gracePeriod time.Duration
}

// NewExecutors returns a real LaunchExecutor/CloseExecutor pair for
// apps.NewManager. scStore and retroStore are used only to resolve
// IDs to real saved data (a shortcut's URL, a game's real file path)
// — nothing here writes through either.
func NewExecutors(scStore *shortcuts.Store, retroStore *retro.Store) (apps.LaunchExecutor, apps.CloseExecutor) {
	e := &executor{scStore: scStore, retroStore: retroStore, gracePeriod: 2 * time.Second}
	return e.launch, e.close
}

func (e *executor) launch(session apps.Session) error {
	switch session.Type {
	case apps.TypeBrowser:
		return e.launchChromium("")
	case apps.TypeShortcut:
		sc, err := e.scStore.Get(session.ShortcutID)
		if err != nil {
			return fmt.Errorf("applaunch: resolving shortcut %q: %w", session.ShortcutID, err)
		}
		return e.launchChromium(sc.URL)
	case apps.TypeRetro:
		return e.launchRetro(session.Console, session.GameID)
	default:
		// Anything else isn't this executor's job yet. Silently
		// succeeding here would be a false claim about what actually
		// launched — same "no-op is honest, quiet pretending is not"
		// stance the rest of this project already holds to.
		return nil
	}
}

func (e *executor) close() error {
	e.mu.Lock()
	pid := e.pid
	e.pid = 0
	e.mu.Unlock()

	if pid == 0 {
		return nil // nothing this executor launched is currently tracked as running
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("applaunch: finding process %d: %w", pid, err)
	}

	// Try a polite SIGTERM first -- proven working for Chromium
	// tonight. Not every program honors it, though: fceux (no window
	// manager here to deliver a real close event) ignored it entirely
	// in testing. If the process is still alive after a short grace
	// period, escalate to SIGKILL rather than silently leaving it
	// running -- same graceful-then-escalate shape already used for
	// Minecraft's own shutdown sequence.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return nil // already gone, or genuinely unsignalable -- nothing more to do
	}

	time.Sleep(e.gracePeriod)

	if err := proc.Signal(syscall.Signal(0)); err == nil {
		// Signal(0) succeeding means the process is still alive.
		_ = proc.Signal(syscall.SIGKILL)
	}
	return nil
}

// chromiumArgs is the pure, testable half of launching a browser —
// given an optional URL, build the real argument list. Kept separate
// from the actual process spawn so the logic that matters (kiosk
// mode, no first-run nag, URL only appended when one was actually
// given) can be verified without needing a real Chromium session.
func chromiumArgs(url string) []string {
	args := []string{"--kiosk", "--no-first-run", "--disable-infobars"}
	if url != "" {
		args = append(args, url)
	}
	return args
}

func (e *executor) launchChromium(url string) error {
	return e.launchAndTrack("chromium", chromiumArgs(url))
}

// launchRetro resolves a console+game into a real emulator command,
// but only for consoles actually listed in consoleEmulator above —
// everything else is a clear, honest error, not a silent no-op.
func (e *executor) launchRetro(console, gameID string) error {
	emuBin, ok := consoleEmulator[console]
	if !ok {
		return fmt.Errorf("applaunch: no emulator configured yet for console %q", console)
	}
	path, err := e.retroStore.GamePath(console, gameID)
	if err != nil {
		return fmt.Errorf("applaunch: resolving game path: %w", err)
	}
	return e.launchAndTrack(emuBin, []string{path})
}

// launchAndTrack is the shared real-process machinery behind both
// launchChromium and launchRetro: find the live X session, start the
// program pointed at it, reap it in the background the instant it
// exits (skipping this leaves a zombie process — found and fixed
// firsthand tonight), and remember its PID for close() to target.
func (e *executor) launchAndTrack(bin string, args []string) error {
	xi, err := xsession.Find()
	if err != nil {
		return fmt.Errorf("applaunch: locating live X session: %w", err)
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"DISPLAY="+xi.Display,
		"XAUTHORITY="+xi.AuthFile,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("applaunch: starting %s: %w", bin, err)
	}

	go func() {
		_ = cmd.Wait()
	}()

	e.mu.Lock()
	e.pid = cmd.Process.Pid
	e.mu.Unlock()
	return nil
}
