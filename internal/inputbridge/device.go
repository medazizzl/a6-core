package inputbridge

import (
	"fmt"

	"github.com/bendahl/uinput"
)

// Device wraps a real virtual keyboard+mouse. bendahl/uinput exposes
// separate keyboard and mouse device types rather than one combo
// device -- so "one combo device" from the blueprint's §5 is
// implemented as two uinput devices created and owned together by
// this struct, presented to the rest of A6 Core as a single unit.
// Callers never see the split; TranslateKeyCode/TranslateMouseButton
// (keymap.go) stay the single source of truth either way.
type Device struct {
	keyboard uinput.Keyboard
	mouse    uinput.Mouse
}

// Open creates the virtual devices. This is the exact point where
// this wave's mandatory hardware check either succeeds or fails --
// deliberately not wrapped in any fallback or retry logic here,
// since a failure here needs to be loud and diagnosable, not
// papered over.
func Open() (*Device, error) {
	kb, err := uinput.CreateKeyboard("/dev/uinput", []byte("a6core-keyboard"))
	if err != nil {
		return nil, fmt.Errorf("inputbridge: creating virtual keyboard: %w", err)
	}
	ms, err := uinput.CreateMouse("/dev/uinput", []byte("a6core-mouse"))
	if err != nil {
		kb.Close()
		return nil, fmt.Errorf("inputbridge: creating virtual mouse: %w", err)
	}
	return &Device{keyboard: kb, mouse: ms}, nil
}

func (d *Device) Close() error {
	kbErr := d.keyboard.Close()
	msErr := d.mouse.Close()
	if kbErr != nil {
		return fmt.Errorf("inputbridge: closing keyboard: %w", kbErr)
	}
	if msErr != nil {
		return fmt.Errorf("inputbridge: closing mouse: %w", msErr)
	}
	return nil
}

// KeyDown/KeyUp take a W3C code, translate it via keymap.go, and
// inject the corresponding evdev event. Translation errors surface
// directly -- an unmapped key from a real phone should be visible,
// not silently dropped.
func (d *Device) KeyDown(w3cCode string) error {
	code, err := TranslateKeyCode(w3cCode)
	if err != nil {
		return err
	}
	return d.keyboard.KeyDown(code)
}

func (d *Device) KeyUp(w3cCode string) error {
	code, err := TranslateKeyCode(w3cCode)
	if err != nil {
		return err
	}
	return d.keyboard.KeyUp(code)
}

// MouseMove takes relative deltas per the blueprint's pointer_move
// schema -- no absolute coordinates, matching the touchpad metaphor.
func (d *Device) MouseMove(dx, dy int32) error {
	return d.mouse.Move(dx, dy)
}

func (d *Device) MouseButtonDown(button string) error {
	switch button {
	case "left":
		return d.mouse.LeftPress()
	case "right":
		return d.mouse.RightPress()
	case "middle":
		return d.mouse.MiddlePress()
	default:
		return &ErrUnknownMouseButton{Button: button}
	}
}

func (d *Device) MouseButtonUp(button string) error {
	switch button {
	case "left":
		return d.mouse.LeftRelease()
	case "right":
		return d.mouse.RightRelease()
	case "middle":
		return d.mouse.MiddleRelease()
	default:
		return &ErrUnknownMouseButton{Button: button}
	}
}

func (d *Device) Scroll(dy int32) error {
	return d.mouse.Wheel(false, dy)
}