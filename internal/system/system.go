package system

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"a6core/internal/resources"
)

type SystemInfo struct {
	Hostname      string `json:"hostname"`
	KernelVersion string `json:"kernel_version"`
	A6CoreVersion string `json:"a6core_version"`
	CPUModel      string `json:"cpu_model"`
	RAMTotalMB    uint64 `json:"ram_total_mb"`
	DiskTotalGB   uint64 `json:"disk_total_gb"`
}

type ActionExecutor func(action string) error

func StubExecutor(action string) error { return nil }

type Manager struct {
	version     string
	checker     resources.Checker
	executor    ActionExecutor
	sleepSetter func(force bool) error
}

func NewManager(version string, checker resources.Checker, executor ActionExecutor, sleepSetter func(force bool) error) *Manager {
	if checker == nil {
		checker = resources.NoBlocking
	}
	if executor == nil {
		executor = StubExecutor
	}
	return &Manager{
		version:     version,
		checker:     checker,
		executor:    executor,
		sleepSetter: sleepSetter,
	}
}

func (m *Manager) Info() (SystemInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	var uname syscall.Utsname
	kernelVersion := "unknown"
	if err := syscall.Uname(&uname); err == nil {
		kernelVersion = unameToString(uname.Release[:])
	}

	cpuModel := readCPUModel()
	ramTotal := readRAMTotalMB()
	diskTotal := readDiskTotalGB()

	return SystemInfo{
		Hostname:      hostname,
		KernelVersion: kernelVersion,
		A6CoreVersion: m.version,
		CPUModel:      cpuModel,
		RAMTotalMB:    ramTotal,
		DiskTotalGB:   diskTotal,
	}, nil
}

func (m *Manager) Reboot(force bool) error {
	return m.action("reboot", force)
}

func (m *Manager) Shutdown(force bool) error {
	return m.action("shutdown", force)
}

func (m *Manager) Suspend(force bool) error {
	if m.sleepSetter != nil {
		if err := m.sleepSetter(force); err != nil {
			return err
		}
	}
	return m.action("suspend", force)
}

func (m *Manager) action(name string, force bool) error {
	if !force {
		if blocking := m.checker(); len(blocking) > 0 {
			return &resources.ErrBlocked{Blocking: blocking}
		}
	}
	if err := m.executor(name); err != nil {
		return fmt.Errorf("system: %s failed: %w", name, err)
	}
	return nil
}

func unameToString(raw []int8) string {
	buf := make([]byte, 0, len(raw))
	for _, b := range raw {
		if b == 0 {
			break
		}
		buf = append(buf, byte(b))
	}
	return string(buf)
}

func readCPUModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Processor") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func readRAMTotalMB() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				var kb uint64
				fmt.Sscanf(fields[1], "%d", &kb)
				return kb / 1024
			}
		}
	}
	return 0
}

func readDiskTotalGB() uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0
	}
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	return totalBytes / (1024 * 1024 * 1024)
}
