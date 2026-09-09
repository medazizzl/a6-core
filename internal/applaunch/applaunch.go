// Package applaunch provides A6 Core's real launch/close executors
// for internal/apps' browser and shortcut session types. Retro and
// Minecraft launching are separate, later work — this package
// deliberately does not touch either.
package applaunch

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"a6core/internal/apps"
	"a6core/internal/shortcuts"
	"a6core/internal/xsession"
)

// executor holds the one piece of state a real launcher needs beyond
// what apps.Session already carries: which real OS process (if any)
// it most recently started, so Close can stop precisely that one —
// not a name-based kill, which we found firsthand tonight can be
// unreliable.
type executor struct {
	scStore *shortcuts.Store
	mu      sync.Mutex
	pid     int
}

// NewExecutors returns a real LaunchExecutor/CloseExecutor pair for
// apps.NewManager. scStore is used only to resolve a shortcut ID to
// its real saved URL — nothing here writes through it.
func NewExecutors(scStore *shortcuts.Store) (apps.LaunchExecutor, apps.CloseExecutor) {
	e := &executor{scStore: scStore}
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
	default:
		// Retro and anything else aren't this executor's job yet.
		// Silently succeeding here would be a false claim about what
		// actually launched — same "no-op is honest, quiet pretending
		// is not" stance the rest of this project already holds to.
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
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("applaunch: signaling process %d: %w", pid, err)
	}
	return nil
}

// chromiumArgs is the pure, testable half of launching — given an
// optional URL, build the real argument list. Kept separate from the
// actual process spawn so the logic that matters (kiosk mode, no
// first-run nag, URL only appended when one was actually given) can
// be verified without needing a real Chromium or X session.
func chromiumArgs(url string) []string {
	args := []string{"--kiosk", "--no-first-run", "--disable-infobars"}
	if url != "" {
		args = append(args, url)
	}
	return args
}

func (e *executor) launchChromium(url string) error {
	xi, err := xsession.Find()
	if err != nil {
		return fmt.Errorf("applaunch: locating live X session: %w", err)
	}

	cmd := exec.Command("chromium", chromiumArgs(url)...)
	cmd.Env = append(os.Environ(),
		"DISPLAY="+xi.Display,
		"XAUTHORITY="+xi.AuthFile,
	)
	// Detached: this executor does not wait for or own Chromium's
	// lifetime beyond starting it — close() above is what stops it.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("applaunch: starting chromium: %w", err)
	}

	// Without this, the process becomes a zombie the instant it exits
	// (whether from close() below or a crash) -- Go never collects a
	// Start()-ed process's exit status unless something calls Wait().
	// Found firsthand: our first real close() left exactly this behind.
	go func() {
		_ = cmd.Wait()
	}()

	e.mu.Lock()
	e.pid = cmd.Process.Pid
	e.mu.Unlock()
	return nil
}
