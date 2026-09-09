package applaunch

import (
	"reflect"
	"testing"
)

func TestChromiumArgsPlainBrowser(t *testing.T) {
	got := chromiumArgs("")
	want := []string{"--kiosk", "--no-first-run", "--disable-infobars"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chromiumArgs(\"\") = %v, want %v", got, want)
	}
}

func TestChromiumArgsWithURL(t *testing.T) {
	got := chromiumArgs("https://example.com")
	want := []string{"--kiosk", "--no-first-run", "--disable-infobars", "https://example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chromiumArgs(url) = %v, want %v", got, want)
	}
}

func TestCloseWithNothingLaunchedIsNoOp(t *testing.T) {
	e := &executor{}
	if err := e.close(); err != nil {
		t.Fatalf("expected close() with no launched process to be a no-op, got: %v", err)
	}
}
