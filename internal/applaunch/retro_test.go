package applaunch

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestLaunchRetroUnknownConsoleReturnsError(t *testing.T) {
	e := &executor{}
	if err := e.launchRetro("snes", "somegame.sfc"); err == nil {
		t.Fatal("expected an error for a console with no configured emulator, got nil")
	}
}

// TestCloseEscalatesToSigkillWhenProcessIgnoresSigterm is a real,
// live process test -- found firsthand tonight that fceux genuinely
// ignores SIGTERM, so this proves the SIGKILL escalation actually
// works, using a real child process that ignores it on purpose,
// rather than assuming the fix is correct.
func TestCloseEscalatesToSigkillWhenProcessIgnoresSigterm(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' TERM; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	go func() { _ = cmd.Wait() }()

	e := &executor{pid: cmd.Process.Pid, gracePeriod: 50 * time.Millisecond}
	if err := e.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // let the OS actually report the kill
	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("expected process to be dead after close(), but it is still alive")
	}
}
