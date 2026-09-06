package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestReadCPUSampleFromRealProcStat(t *testing.T) {
	// Deliberately reads the REAL /proc/stat (no fixture) — this is
	// specifically to catch a parsing assumption that's wrong for
	// the real kernel on the real Acer, the exact class of bug that
	// bit the icons.go favicon fetcher (worked in mocks, failed on
	// the real GitHub .ico). No injectable path here on purpose.
	sample, err := readCPUSample()
	if err != nil {
		t.Fatalf("readCPUSample: %v", err)
	}
	if sample.total == 0 {
		t.Fatal("expected a non-zero total from /proc/stat")
	}
	if sample.idle > sample.total {
		t.Fatalf("idle (%d) must not exceed total (%d)", sample.idle, sample.total)
	}
}

func TestCPUPercentNotReadyBeforeTwoSamples(t *testing.T) {
	s := NewSampler(10 * time.Millisecond)
	if _, ok := s.CPUPercent(); ok {
		t.Fatal("expected CPUPercent to report not-ready before any samples exist")
	}
}

func TestCPUPercentBecomesReadyAfterSampling(t *testing.T) {
	s := NewSampler(10 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := s.CPUPercent(); ok {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("CPUPercent never became ready after running the sampler")
}

func TestCPUPercentWithinPlausibleRange(t *testing.T) {
	s := NewSampler(10 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go s.Run(ctx)
	time.Sleep(150 * time.Millisecond)

	pct, ok := s.CPUPercent()
	if !ok {
		t.Fatal("expected CPUPercent to be ready by now")
	}
	if pct < 0 || pct > 100 {
		t.Fatalf("CPU percent out of plausible range: %f", pct)
	}
}

func TestReadMemInfoFromRealProcMeminfo(t *testing.T) {
	// Same principle as the CPU test above: reads the real
	// /proc/meminfo on whatever machine actually runs this, no
	// fixture, to catch a real parsing mismatch rather than a
	// theoretical one.
	mem, err := ReadMemInfo()
	if err != nil {
		t.Fatalf("ReadMemInfo: %v", err)
	}
	if mem.RAMTotalMB == 0 {
		t.Fatal("expected non-zero RAMTotalMB")
	}
	if mem.RAMUsedMB > mem.RAMTotalMB {
		t.Fatalf("RAMUsedMB (%d) must not exceed RAMTotalMB (%d)", mem.RAMUsedMB, mem.RAMTotalMB)
	}
}

func TestDiskFreeGBOnRealRoot(t *testing.T) {
	free, err := DiskFreeGB("/")
	if err != nil {
		t.Fatalf("DiskFreeGB: %v", err)
	}
	if free == 0 {
		t.Fatal("expected non-zero free disk space on / — if this is genuinely near-full, this assumption needs revisiting")
	}
}

func TestUptimeSecondsIsPositive(t *testing.T) {
	uptime, err := UptimeSeconds()
	if err != nil {
		t.Fatalf("UptimeSeconds: %v", err)
	}
	if uptime == 0 {
		t.Fatal("expected non-zero uptime on any machine that's been running long enough to run tests")
	}
}

func TestTemperatureReturnsHonestOkFlag(t *testing.T) {
	// No assertion on the VALUE — this hardware's sensor path has
	// never been confirmed. The only thing worth asserting is that
	// the function does not panic and behaves consistently: if ok is
	// false, celsius must be exactly the zero value, never a
	// fabricated non-zero number pretending to be real.
	celsius, ok := Temperature()
	if !ok && celsius != 0 {
		t.Fatalf("when ok=false, celsius must be 0, got %f", celsius)
	}
	t.Logf("Temperature() on this machine: celsius=%f ok=%v", celsius, ok)
}

func TestLocalNetworkReturnsHonestOkFlag(t *testing.T) {
	// Same reasoning as Temperature() above: real network reachability
	// varies by environment (a CI sandbox's egress rules are not the
	// real Acer's real LAN), so this only asserts internal consistency
	// -- ok=false must mean genuinely empty strings, never a fabricated
	// address -- and logs the real result for visibility rather than
	// hard-failing on a specific IP.
	ip, iface, ok := LocalNetwork()
	if !ok && (ip != "" || iface != "") {
		t.Fatalf("when ok=false, ip and iface must both be empty, got ip=%q iface=%q", ip, iface)
	}
	if ok && ip == "" {
		t.Fatal("ok=true must come with a non-empty ip")
	}
	t.Logf("LocalNetwork() on this machine: ip=%q iface=%q ok=%v", ip, iface, ok)
}