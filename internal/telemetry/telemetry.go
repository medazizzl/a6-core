// Package telemetry provides live system metrics for the frozen
// spec's Status schema (§8) and status.tick WebSocket event (§7).
// Everything here reads directly from /proc and /sys — no
// subprocess spawning (no `top`, no `free`), consistent with this
// project's standing hardware-conscious discipline since Stage 9.
package telemetry

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// cpuSample is one instantaneous reading of the cumulative counters
// on /proc/stat's aggregate "cpu" line. CPU percentage is only
// meaningful as a DELTA between two samples — see Sampler below.
type cpuSample struct {
	idle, total uint64
	at          time.Time
}

func readCPUSample() (cpuSample, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, fmt.Errorf("telemetry: opening /proc/stat: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return cpuSample{}, errors.New("telemetry: /proc/stat is empty")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, fmt.Errorf("telemetry: unexpected /proc/stat format: %q", scanner.Text())
	}

	var total uint64
	var idle uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue // /proc/stat has grown extra columns across kernel versions; ignore ones we don't parse
		}
		total += v
		if i == 3 { // the 4th value (index 3) is "idle" per the documented column order
			idle = v
		}
	}
	return cpuSample{idle: idle, total: total, at: time.Now()}, nil
}

// Sampler keeps a rolling background CPU sample so CPUPercent() is
// always instant, never blocking a caller on a sampling window.
// Deliberately not tied to the HTTP request lifecycle at all — this
// mirrors why Stage 12 kept server/app state out of state.json: live
// system numbers belong to their own small, honest subsystem.
type Sampler struct {
	mu       sync.RWMutex
	previous *cpuSample
	current  *cpuSample
	interval time.Duration
}

// NewSampler: interval controls how often the background reading
// happens. 0 means "use the real 1-second default"; tests use a
// much shorter interval so they don't take a full second each.
func NewSampler(interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = time.Second
	}
	return &Sampler{interval: interval}
}

// Run drives the background sampling loop until ctx is cancelled —
// started as its own goroutine from main.go (Wave 4), same pattern
// as events.Hub.Run.
func (s *Sampler) Run(ctx context.Context) {
	s.tick() // take a first sample immediately so early callers aren't stuck at 0
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Sampler) tick() {
	sample, err := readCPUSample()
	if err != nil {
		return // leave previous/current as they were; a transient read failure shouldn't erase good data
	}
	s.mu.Lock()
	s.previous = s.current
	s.current = &sample
	s.mu.Unlock()
}

// CPUPercent returns the delta-based CPU usage between the two most
// recent samples. Returns (0, false) until at least two samples
// exist — an honest "not ready yet" rather than a fake 0%.
func (s *Sampler) CPUPercent() (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.previous == nil || s.current == nil {
		return 0, false
	}
	totalDelta := s.current.total - s.previous.total
	idleDelta := s.current.idle - s.previous.idle
	if totalDelta == 0 {
		return 0, false
	}
	busy := float64(totalDelta-idleDelta) / float64(totalDelta)
	return busy * 100, true
}

// MemInfo mirrors the fields the frozen Status schema needs (§8):
// ram_used_mb/ram_total_mb/swap_used_mb.
type MemInfo struct {
	RAMTotalMB uint64
	RAMUsedMB  uint64
	SwapUsedMB uint64
}

// ReadMemInfo parses /proc/meminfo directly. "Used" is computed the
// same way tools like `free` do: MemTotal - MemAvailable, which
// correctly accounts for reclaimable cache/buffers rather than
// naively reporting them as "used" (which would look artificially
// alarming on a system that's using cache well, not badly).
func ReadMemInfo() (MemInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemInfo{}, fmt.Errorf("telemetry: opening /proc/meminfo: %w", err)
	}
	defer f.Close()

	values := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = v // all these fields are in kB per the /proc/meminfo format
	}

	total, hasTotal := values["MemTotal"]
	available, hasAvailable := values["MemAvailable"]
	swapTotal := values["SwapTotal"]
	swapFree := values["SwapFree"]

	if !hasTotal {
		return MemInfo{}, errors.New("telemetry: MemTotal missing from /proc/meminfo")
	}
	var usedKB uint64
	if hasAvailable && available <= total {
		usedKB = total - available
	}
	var swapUsedKB uint64
	if swapTotal >= swapFree {
		swapUsedKB = swapTotal - swapFree
	}

	return MemInfo{
		RAMTotalMB: total / 1024,
		RAMUsedMB:  usedKB / 1024,
		SwapUsedMB: swapUsedKB / 1024,
	}, nil
}

// DiskFreeGB reuses the exact syscall.Statfs pattern Stage 9's
// system.go already established for disk totals — same call, same
// discipline, just reporting free space (Bavail: blocks available to
// an unprivileged user, the honest "how much can actually be used"
// figure) instead of total capacity.
func DiskFreeGB(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("telemetry: statfs %q: %w", path, err)
	}
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	return freeBytes / (1024 * 1024 * 1024), nil
}

// UptimeSeconds reads /proc/uptime directly — already a duration
// since boot, not a cumulative counter, so no sampling is needed
// (unlike CPU).
func UptimeSeconds() (uint64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("telemetry: reading /proc/uptime: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, errors.New("telemetry: /proc/uptime empty")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("telemetry: parsing /proc/uptime: %w", err)
	}
	return uint64(seconds), nil
}

// LocalNetwork reports this machine's primary outbound IP address and
// the network interface it's reachable through. Uses the same "dial
// out, see which local address the kernel picks for the route" trick
// this project already relies on elsewhere (the `ip route get
// 1.1.1.1` workaround for this minimal Arch install having no
// `hostname` command) — no packets are actually sent, a UDP "dial"
// just resolves a route without a handshake. Returns ok=false rather
// than a fabricated empty-string network, matching Temperature()'s
// "don't fake a reading" discipline: on a machine with Wi-Fi and
// Ethernet both down, there genuinely is no answer.
func LocalNetwork() (ip string, iface string, ok bool) {
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return "", "", false
	}
	defer conn.Close()

	udpAddr, isUDP := conn.LocalAddr().(*net.UDPAddr)
	if !isUDP {
		return "", "", false
	}
	ip = udpAddr.IP.String()

	// Naming the interface is a nice-to-have (useful context on a
	// machine with known Wi-Fi reliability issues, where "am I on
	// Ethernet or Wi-Fi right now" genuinely matters) — if it can't be
	// determined for any reason, the IP alone is still real and useful,
	// so this degrades gracefully rather than failing the whole call.
	ifaces, err := net.Interfaces()
	if err != nil {
		return ip, "", true
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var addrIP net.IP
			switch v := a.(type) {
			case *net.IPNet:
				addrIP = v.IP
			case *net.IPAddr:
				addrIP = v.IP
			}
			if addrIP != nil && addrIP.String() == ip {
				return ip, i.Name, true
			}
		}
	}
	return ip, "", true
}

const hwmonRoot = "/sys/class/hwmon"

// Temperature scans the standard hwmon sysfs tree for the first
// readable temperature sensor. Returns ok=false rather than 0 when
// nothing is found or nothing parses — this hardware's exact sensor
// path has never been confirmed (unlike every other figure in this
// project, which traces back to something explicitly verified
// earlier), so silently returning 0 would look like a real cold
// reading instead of "we don't actually know."
func Temperature() (celsius float64, ok bool) {
	entries, err := os.ReadDir(hwmonRoot)
	if err != nil {
		return 0, false
	}
	for _, entry := range entries {
		hwmonDir := filepath.Join(hwmonRoot, entry.Name())
		matches, _ := filepath.Glob(filepath.Join(hwmonDir, "temp*_input"))
		for _, path := range matches {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			milliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil {
				continue
			}
			return float64(milliC) / 1000.0, true
		}
	}
	return 0, false
}