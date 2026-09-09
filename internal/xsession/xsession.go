// Package xsession locates the currently running X server's display
// and auth file, purely by reading /proc — no shell-exec, no
// assumption about how or when Xorg was started (manually, by the
// TV shell, or anything else). This is what lets A6 Core launch real
// GUI programs (Chromium today, future emulators) into whatever X
// session is actually live right now, without any fixed-path
// convention or coordination required from whoever started it.
package xsession

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrNoXServer means no running Xorg process was found at all.
var ErrNoXServer = errors.New("xsession: no running Xorg process found")

// Info is everything a launched GUI program needs to actually reach
// the live X session.
type Info struct {
	Display  string // e.g. ":0"
	AuthFile string // e.g. /tmp/serverauth.XXXXXXXXXX
}

// Find scans /proc for a running Xorg process and extracts its
// display name and -auth file path directly from its real command
// line — the same information "ps aux | grep Xorg" would show a
// human, just read without shelling out to ps or grep at all.
func Find() (Info, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Info{}, fmt.Errorf("xsession: reading /proc: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a PID directory
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue // process may have exited between listing and reading; not worth surfacing
		}
		if strings.TrimSpace(string(comm)) != "Xorg" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		info := parseXorgCmdline(args)
		if info.AuthFile == "" {
			return Info{}, fmt.Errorf("xsession: found Xorg (pid %s) but no -auth argument in its command line", e.Name())
		}
		return info, nil
	}
	return Info{}, ErrNoXServer
}

// parseXorgCmdline is the pure, testable half of Find — given the
// real argv Xorg was started with, extract the display name and the
// -auth file path. Split out so this logic can be verified without
// needing a real, running X server.
func parseXorgCmdline(args []string) Info {
	info := Info{Display: ":0"} // fallback default if none is explicitly listed
	for i, a := range args {
		if len(a) > 1 && a[0] == ':' && a[1] >= '0' && a[1] <= '9' {
			info.Display = a
		}
		if a == "-auth" && i+1 < len(args) {
			info.AuthFile = args[i+1]
		}
	}
	return info
}
