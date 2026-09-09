package xsession

import "testing"

func TestParseXorgCmdlineFindsDisplayAndAuth(t *testing.T) {
	args := []string{"/usr/lib/Xorg", ":0", "vt1", "-auth", "/tmp/serverauth.ABC123"}
	info := parseXorgCmdline(args)
	if info.Display != ":0" {
		t.Fatalf("expected display %q, got %q", ":0", info.Display)
	}
	if info.AuthFile != "/tmp/serverauth.ABC123" {
		t.Fatalf("expected auth file %q, got %q", "/tmp/serverauth.ABC123", info.AuthFile)
	}
}

func TestParseXorgCmdlineDifferentDisplayNumber(t *testing.T) {
	args := []string{"/usr/lib/Xorg", ":1", "-auth", "/tmp/serverauth.XYZ"}
	info := parseXorgCmdline(args)
	if info.Display != ":1" {
		t.Fatalf("expected display %q, got %q", ":1", info.Display)
	}
}

func TestParseXorgCmdlineMissingAuthLeavesItEmpty(t *testing.T) {
	args := []string{"/usr/lib/Xorg", ":0", "vt1"}
	info := parseXorgCmdline(args)
	if info.AuthFile != "" {
		t.Fatalf("expected empty auth file, got %q", info.AuthFile)
	}
	if info.Display != ":0" {
		t.Fatalf("expected display %q, got %q", ":0", info.Display)
	}
}
