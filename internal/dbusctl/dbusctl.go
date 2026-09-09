// Package dbusctl is A6 Core's entire D-Bus surface — deliberately
// small and specific. It talks to systemd-logind
// (org.freedesktop.login1) for exactly two actions: Reboot,
// PowerOff. It also talks to systemd itself
// (org.freedesktop.systemd1) to start/stop/query exactly one
// allowlisted unit (see allowedUnits below) — added for the real
// Minecraft (Bedrock) server executor, Stage 18. There is no general
// "call any D-Bus method" or "manage any unit" capability anywhere in
// this package or exposed above it — this maintains the same "never
// expose arbitrary execution" boundary from the frozen spec (§15),
// extended from shell commands to cover D-Bus as well.
package dbusctl

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	loginDest    = "org.freedesktop.login1"
	loginPath    = "/org/freedesktop/login1"
	loginIface   = "org.freedesktop.login1.Manager"
	systemdDest  = "org.freedesktop.systemd1"
	systemdPath  = "/org/freedesktop/systemd1"
	systemdIface = "org.freedesktop.systemd1.Manager"
	unitIface    = "org.freedesktop.systemd1.Unit"
	callTimeout  = 5 * time.Second
)

// allowedUnits is the ONLY set of systemd units this package will
// ever start, stop, or query the state of — a fixed allowlist, not a
// parameter a caller can widen. Adding a new manageable service means
// explicitly adding it here and to the matching polkit rule, not just
// passing a new string in from somewhere else in the codebase.
var allowedUnits = map[string]bool{
	"minecraft-bedrock-server.service": true,
}

// Client wraps a connection to the system D-Bus bus. Not the session
// bus — logind and systemd1 both live on the system bus, and A6 Core
// runs as a systemd system service with no session bus to speak of
// anyway.
type Client struct {
	conn *dbus.Conn
}

func Connect() (*Client, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("dbusctl: connecting to system bus: %w", err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// call issues one logind Manager method with a bound timeout.
// interactive is always false — see package-level design notes: A6
// Core is a headless service with no session to show a polkit
// authorization prompt in, so allowing an interactive prompt would
// risk silently hanging instead of failing loudly.
func (c *Client) call(ctx context.Context, method string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	obj := c.conn.Object(loginDest, dbus.ObjectPath(loginPath))
	call := obj.CallWithContext(ctx, loginIface+"."+method, 0, false /* interactive */)
	if call.Err != nil {
		return fmt.Errorf("dbusctl: %s failed: %w", method, call.Err)
	}
	return nil
}

func (c *Client) Reboot(ctx context.Context) error {
	return c.call(ctx, "Reboot")
}

func (c *Client) PowerOff(ctx context.Context) error {
	return c.call(ctx, "PowerOff")
}

// CanReboot, CanPowerOff query logind for whether the
// action is currently permitted, WITHOUT performing it — this is
// what a "dry run" actually means for a D-Bus action: there is no
// generic no-op flag on these methods, so verifying permission via
// logind's own Can* queries is the honest equivalent, not a fake
// simulation of one.
func (c *Client) CanReboot(ctx context.Context) (string, error) {
	return c.canQuery(ctx, "CanReboot")
}

func (c *Client) CanPowerOff(ctx context.Context) (string, error) {
	return c.canQuery(ctx, "CanPowerOff")
}

func (c *Client) canQuery(ctx context.Context, method string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	obj := c.conn.Object(loginDest, dbus.ObjectPath(loginPath))
	call := obj.CallWithContext(ctx, loginIface+"."+method, 0)
	if call.Err != nil {
		return "", fmt.Errorf("dbusctl: %s failed: %w", method, call.Err)
	}
	var result string
	if err := call.Store(&result); err != nil {
		return "", fmt.Errorf("dbusctl: %s: unexpected response shape: %w", method, err)
	}
	return result, nil // one of: "yes", "no", "challenge", "na" — per logind's own documented values
}

// StartUnit and StopUnit start or stop an allowlisted systemd unit via
// systemd's own D-Bus API — never by shelling out to `systemctl`,
// per the frozen spec's "A6 Core never executes arbitrary shell
// commands" rule (§15), which this extends to cover process/service
// management the same way it already covers reboot/poweroff. "replace"
// mode matches what `systemctl start`/`stop` do by default: queue the
// job, replacing any conflicting queued job for the same unit.
func (c *Client) StartUnit(ctx context.Context, unit string) error {
	if !allowedUnits[unit] {
		return fmt.Errorf("dbusctl: refusing to start unit %q (not in allowlist)", unit)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	obj := c.conn.Object(systemdDest, dbus.ObjectPath(systemdPath))
	call := obj.CallWithContext(ctx, systemdIface+".StartUnit", 0, unit, "replace")
	if call.Err != nil {
		return fmt.Errorf("dbusctl: StartUnit(%s) failed: %w", unit, call.Err)
	}
	return nil
}

func (c *Client) StopUnit(ctx context.Context, unit string) error {
	if !allowedUnits[unit] {
		return fmt.Errorf("dbusctl: refusing to stop unit %q (not in allowlist)", unit)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	obj := c.conn.Object(systemdDest, dbus.ObjectPath(systemdPath))
	call := obj.CallWithContext(ctx, systemdIface+".StopUnit", 0, unit, "replace")
	if call.Err != nil {
		return fmt.Errorf("dbusctl: StopUnit(%s) failed: %w", unit, call.Err)
	}
	return nil
}

// UnitActiveState returns systemd's own ActiveState string for an
// allowlisted unit (e.g. "active", "inactive", "activating",
// "deactivating", "failed") — the honest source of truth for whether
// something is really running, rather than trusting an in-memory
// guess. If the unit has never been loaded this boot, GetUnit itself
// fails — that's a real, valid "inactive" state, not an error worth
// surfacing as one.
func (c *Client) UnitActiveState(ctx context.Context, unit string) (string, error) {
	if !allowedUnits[unit] {
		return "", fmt.Errorf("dbusctl: refusing to query unit %q (not in allowlist)", unit)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	obj := c.conn.Object(systemdDest, dbus.ObjectPath(systemdPath))
	call := obj.CallWithContext(ctx, systemdIface+".GetUnit", 0, unit)
	if call.Err != nil {
		return "inactive", nil
	}
	var unitPath dbus.ObjectPath
	if err := call.Store(&unitPath); err != nil {
		return "", fmt.Errorf("dbusctl: GetUnit(%s): unexpected response shape: %w", unit, err)
	}

	unitObj := c.conn.Object(systemdDest, unitPath)
	prop, err := unitObj.GetProperty(unitIface + ".ActiveState")
	if err != nil {
		return "", fmt.Errorf("dbusctl: reading ActiveState for %s: %w", unit, err)
	}
	state, ok := prop.Value().(string)
	if !ok {
		return "", fmt.Errorf("dbusctl: ActiveState for %s was not a string (got %T)", unit, prop.Value())
	}
	return state, nil
}