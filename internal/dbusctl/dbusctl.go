// Package dbusctl is A6 Core's entire D-Bus surface — deliberately
// small and specific. It talks to systemd-logind
// (org.freedesktop.login1) for exactly two actions: Reboot,
// PowerOff. There is no general "call any D-Bus method"
// capability anywhere in this package or exposed above it — this
// maintains the same "never expose arbitrary execution" boundary
// from the frozen spec (§15), extended from shell commands to cover
// D-Bus as well.
package dbusctl

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	loginDest   = "org.freedesktop.login1"
	loginPath   = "/org/freedesktop/login1"
	loginIface  = "org.freedesktop.login1.Manager"
	callTimeout = 5 * time.Second
)

// Client wraps a connection to the system D-Bus bus. Not the session
// bus — logind lives on the system bus, and A6 Core runs as a
// systemd system service with no session bus to speak of anyway.
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