// Package inputbridge implements A6 Core's virtual input device — a
// combined virtual keyboard/mouse per the Stage 17 blueprint §5.
// A6 Core owns this device; nothing else on the machine is meant to
// write to it directly (blueprint's ownership principle, deliberately
// not pinned to a specific process boundary yet).
package inputbridge

import "fmt"

// keyCodeMap translates W3C KeyboardEvent.code values (what a phone's
// native keyboard APIs actually produce) to Linux evdev keycodes
// (what a virtual input device actually needs to inject). This is
// the ONE place this translation happens, per the blueprint's
// explicit intent that the phone app never needs to know anything
// Linux-specific.
//
// Values are the standard evdev KEY_* constants from
// linux/input-event-codes.h, given here as plain ints so this file
// has zero dependency on any specific uinput library's own constant
// names — keeps the translation table testable independent of
// whichever library Wave 1's hardware layer ends up using.
var keyCodeMap = map[string]int{
	"ArrowUp":    103,
	"ArrowDown":  108,
	"ArrowLeft":  105,
	"ArrowRight": 106,
	"Enter":      28,
	"Escape":     1,
	"Backspace":  14,
	"Tab":        15,
	"Space":      57,
	"ShiftLeft":  42,
	"ShiftRight": 54,
	"ControlLeft": 29,
	"AltLeft":    56,

	"KeyA": 30, "KeyB": 48, "KeyC": 46, "KeyD": 32, "KeyE": 18,
	"KeyF": 33, "KeyG": 34, "KeyH": 35, "KeyI": 23, "KeyJ": 36,
	"KeyK": 37, "KeyL": 38, "KeyM": 50, "KeyN": 49, "KeyO": 24,
	"KeyP": 25, "KeyQ": 16, "KeyR": 19, "KeyS": 31, "KeyT": 20,
	"KeyU": 22, "KeyV": 47, "KeyW": 17, "KeyX": 45, "KeyY": 21,
	"KeyZ": 44,

	"Digit0": 11, "Digit1": 2, "Digit2": 3, "Digit3": 4, "Digit4": 5,
	"Digit5": 6, "Digit6": 7, "Digit7": 8, "Digit8": 9, "Digit9": 10,
}

// ErrUnknownKeyCode is returned for any W3C code this project hasn't
// explicitly mapped yet — deliberately a named error, not a silent
// no-op, so an unmapped key press from a real phone is loud and
// diagnosable rather than a mysterious "nothing happened."
type ErrUnknownKeyCode struct {
	Code string
}

func (e *ErrUnknownKeyCode) Error() string {
	return fmt.Sprintf("inputbridge: unrecognized key code %q", e.Code)
}

// TranslateKeyCode converts a W3C KeyboardEvent.code string to its
// evdev keycode equivalent. Pure function — no I/O, no dependency on
// any real input device existing, fully testable in isolation.
func TranslateKeyCode(w3cCode string) (int, error) {
	code, ok := keyCodeMap[w3cCode]
	if !ok {
		return 0, &ErrUnknownKeyCode{Code: w3cCode}
	}
	return code, nil
}

// MouseButton mirrors the schema's pointer_button.button values,
// translated to evdev's own button constants.
var mouseButtonMap = map[string]int{
	"left":   0x110, // BTN_LEFT
	"right":  0x111, // BTN_RIGHT
	"middle": 0x112, // BTN_MIDDLE
}

type ErrUnknownMouseButton struct {
	Button string
}

func (e *ErrUnknownMouseButton) Error() string {
	return fmt.Sprintf("inputbridge: unrecognized mouse button %q", e.Button)
}

func TranslateMouseButton(button string) (int, error) {
	code, ok := mouseButtonMap[button]
	if !ok {
		return 0, &ErrUnknownMouseButton{Button: button}
	}
	return code, nil
}