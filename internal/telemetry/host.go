package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HostTelemetry aggregates machine-level vitals.
type HostTelemetry struct {
	Timestamp              time.Time `json:"timestamp"`
	CPUPercent             float64   `json:"cpuPercent"`
	MemTotalBytes          uint64    `json:"memTotalBytes"`
	MemAvailableBytes      uint64    `json:"memAvailableBytes"`
	MemUsedBytes           uint64    `json:"memUsedBytes"`
	MemUsedPercent         float64   `json:"memUsedPercent"`
	SwapTotalBytes         uint64    `json:"swapTotalBytes"`
	SwapUsedBytes          uint64    `json:"swapUsedBytes"`
	SwapUsedPercent        float64   `json:"swapUsedPercent"`
	DiskTotalBytes         uint64    `json:"diskTotalBytes"`
	DiskFreeBytes          uint64    `json:"diskFreeBytes"`
	DiskUsedBytes          uint64    `json:"diskUsedBytes"`
	DiskUsedPercent        float64   `json:"diskUsedPercent"`
	UptimeSeconds          uint64    `json:"uptimeSeconds"`
	PendingUpdates         int       `json:"pendingUpdates"`
	PendingSecurityUpdates int       `json:"pendingSecurityUpdates"`
	RebootRequired         bool      `json:"rebootRequired"`
}

var (
	prevCPULock sync.Mutex
	prevIdle    uint64
	prevTotal   uint64

	aptCacheLock    sync.Mutex
	lastAptCheck    time.Time
	cachedUpdates   int
	cachedSecUpdate int
)

// ReadCPUPercent reads /proc/stat and calculates total CPU utilisation across all cores.
func ReadCPUPercent() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0.0
	}

	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 {
		return 0.0
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0.0
	}

	var total uint64
	var idle uint64
	for i, f := range fields[1:] {
		val, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		total += val
		if i == 3 { // idle is index 3 (4th field after "cpu")
			idle = val
		}
	}

	prevCPULock.Lock()
	defer prevCPULock.Unlock()

	if prevTotal == 0 || total <= prevTotal {
		prevIdle = idle
		prevTotal = total
		return 0.0
	}

	totalDelta := float64(total - prevTotal)
	idleDelta := float64(idle - prevIdle)
	prevIdle = idle
	prevTotal = total

	if totalDelta <= 0 {
		return 0.0
	}
	cpuUsage := (1.0 - (idleDelta / totalDelta)) * 100.0
	if cpuUsage < 0 {
		cpuUsage = 0
	} else if cpuUsage > 100 {
		cpuUsage = 100
	}
	return cpuUsage
}

// ReadMemInfo reads /proc/meminfo and computes memory & swap usage.
func ReadMemInfo() (memTotal, memAvail, swapTotal, swapFree uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		// /proc/meminfo values are in kB
		valBytes := val * 1024
		switch fields[0] {
		case "MemTotal:":
			memTotal = valBytes
		case "MemAvailable:":
			memAvail = valBytes
		case "SwapTotal:":
			swapTotal = valBytes
		case "SwapFree:":
			swapFree = valBytes
		}
	}
	return
}

// ReadDiskUsage runs statfs on the specified path (or current working directory).
func ReadDiskUsage(path string) (total, free, used uint64, usedPercent float64) {
	if path == "" {
		path = "/"
	}
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil || s.Blocks == 0 {
		return 0, 0, 0, 0.0
	}

	total = s.Blocks * uint64(s.Bsize)
	free = s.Bavail * uint64(s.Bsize)
	if total >= free {
		used = total - free
		usedPercent = float64(used) * 100.0 / float64(total)
	}
	return
}

// ReadUptime reads system uptime in seconds from /proc/uptime.
func ReadUptime() uint64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return uint64(val)
}

// CheckPendingUpdates returns pending packages count and security updates count.
// It caches the result for 15 minutes to avoid excessive subcommands.
func CheckPendingUpdates(ctx context.Context) (total int, security int) {
	// First check /var/lib/update-notifier/updates-available if it exists
	if b, err := os.ReadFile("/var/lib/update-notifier/updates-available"); err == nil {
		lines := strings.Split(string(b), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.Contains(l, "updates can be applied immediately") {
				f := strings.Fields(l)
				if len(f) > 0 {
					total, _ = strconv.Atoi(f[0])
				}
			}
			if strings.Contains(l, "standard security updates") {
				f := strings.Fields(l)
				if len(f) > 0 {
					security, _ = strconv.Atoi(f[0])
				}
			}
		}
		if total > 0 || security > 0 {
			return total, security
		}
	}

	aptCacheLock.Lock()
	defer aptCacheLock.Unlock()

	now := time.Now()
	if !lastAptCheck.IsZero() && now.Sub(lastAptCheck) < 15*time.Minute {
		return cachedUpdates, cachedSecUpdate
	}

	// Simulation via apt-get -s upgrade
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "apt-get", "-s", "upgrade")
	out, err := cmd.Output()
	if err != nil {
		return cachedUpdates, cachedSecUpdate
	}

	var tot, sec int
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		l := scanner.Text()
		if strings.HasPrefix(l, "Inst ") {
			tot++
			if strings.Contains(l, "security") || strings.Contains(l, "Security") {
				sec++
			}
		}
	}

	cachedUpdates = tot
	cachedSecUpdate = sec
	lastAptCheck = now
	return tot, sec
}

// IsRebootRequired checks if /var/run/reboot-required exists.
func IsRebootRequired() bool {
	_, err := os.Stat("/var/run/reboot-required")
	return err == nil
}

// ReadHostTelemetry collects a full snapshot of machine telemetry.
func ReadHostTelemetry(ctx context.Context, dataDir string) HostTelemetry {
	memTotal, memAvail, swapTotal, swapFree := ReadMemInfo()
	memUsed := uint64(0)
	var memPercent float64
	if memTotal > memAvail {
		memUsed = memTotal - memAvail
		memPercent = float64(memUsed) * 100.0 / float64(memTotal)
	}

	swapUsed := uint64(0)
	var swapPercent float64
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
		swapPercent = float64(swapUsed) * 100.0 / float64(swapTotal)
	}

	diskTotal, diskFree, diskUsed, diskPercent := ReadDiskUsage(dataDir)
	totUpdates, secUpdates := CheckPendingUpdates(ctx)

	return HostTelemetry{
		Timestamp:              time.Now(),
		CPUPercent:             ReadCPUPercent(),
		MemTotalBytes:          memTotal,
		MemAvailableBytes:      memAvail,
		MemUsedBytes:           memUsed,
		MemUsedPercent:         memPercent,
		SwapTotalBytes:         swapTotal,
		SwapUsedBytes:          swapUsed,
		SwapUsedPercent:        swapPercent,
		DiskTotalBytes:         diskTotal,
		DiskFreeBytes:          diskFree,
		DiskUsedBytes:          diskUsed,
		DiskUsedPercent:        diskPercent,
		UptimeSeconds:          ReadUptime(),
		PendingUpdates:         totUpdates,
		PendingSecurityUpdates: secUpdates,
		RebootRequired:         IsRebootRequired(),
	}
}
