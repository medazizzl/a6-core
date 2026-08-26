package inputbridge

import (
	"errors"
	"testing"
)

func TestTranslateKeyCodeKnownKeys(t *testing.T) {
	cases := map[string]int{
		"ArrowUp":   103,
		"Enter":     28,
		"KeyA":      30,
		"Digit5":    6,
		"Backspace": 14,
	}
	for code, want := range cases {
		got, err := TranslateKeyCode(code)
		if err != nil {
			t.Fatalf("TranslateKeyCode(%q): unexpected error: %v", code, err)
		}
		if got != want {
			t.Fatalf("TranslateKeyCode(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestTranslateKeyCodeUnknown(t *testing.T) {
	_, err := TranslateKeyCode("SomeFutureKeyNobodyMappedYet")
	if err == nil {
		t.Fatal("expected an error for an unmapped key code, got nil")
	}
	var unknownErr *ErrUnknownKeyCode
	if !errors.As(err, &unknownErr) {
		t.Fatalf("expected *ErrUnknownKeyCode, got %T: %v", err, err)
	}
	if unknownErr.Code != "SomeFutureKeyNobodyMappedYet" {
		t.Fatalf("unexpected code in error: %q", unknownErr.Code)
	}
}

func TestTranslateMouseButtonKnown(t *testing.T) {
	cases := map[string]int{"left": 0x110, "right": 0x111, "middle": 0x112}
	for button, want := range cases {
		got, err := TranslateMouseButton(button)
		if err != nil {
			t.Fatalf("TranslateMouseButton(%q): unexpected error: %v", button, err)
		}
		if got != want {
			t.Fatalf("TranslateMouseButton(%q) = %#x, want %#x", button, got, want)
		}
	}
}

func TestTranslateMouseButtonUnknown(t *testing.T) {
	if _, err := TranslateMouseButton("scroll-wheel-click"); err == nil {
		t.Fatal("expected an error for an unrecognized button, got nil")
	}
}

func TestEveryLetterAndDigitIsMapped(t *testing.T) {
	for c := 'A'; c <= 'Z'; c++ {
		code := "Key" + string(c)
		if _, err := TranslateKeyCode(code); err != nil {
			t.Errorf("expected %q to be mapped, got error: %v", code, err)
		}
	}
	for d := '0'; d <= '9'; d++ {
		code := "Digit" + string(d)
		if _, err := TranslateKeyCode(code); err != nil {
			t.Errorf("expected %q to be mapped, got error: %v", code, err)
		}
	}
}