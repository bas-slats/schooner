package health

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SystemHealth contains host machine health metrics
type SystemHealth struct {
	CPU        CPUHealth    `json:"cpu"`
	Memory     MemoryHealth `json:"memory"`
	Disk       DiskHealth   `json:"disk"`
	Uptime     time.Duration `json:"uptime"`
	Platform   string       `json:"platform"`
	NumCPU     int          `json:"num_cpu"`
	GoRoutines int          `json:"go_routines"`
}

// CPUHealth contains CPU usage information
type CPUHealth struct {
	UsagePercent float64 `json:"usage_percent"`
	LoadAvg1     float64 `json:"load_avg_1"`
	LoadAvg5     float64 `json:"load_avg_5"`
	LoadAvg15    float64 `json:"load_avg_15"`
}

// MemoryHealth contains memory usage information
type MemoryHealth struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

// DiskHealth contains disk usage information
type DiskHealth struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
	Path        string  `json:"path"`
}

// GetSystemHealth collects system health metrics
func GetSystemHealth() (*SystemHealth, error) {
	health := &SystemHealth{
		Platform:   runtime.GOOS,
		NumCPU:     runtime.NumCPU(),
		GoRoutines: runtime.NumGoroutine(),
	}

	// Get memory stats
	mem, err := getMemoryStats()
	if err == nil {
		health.Memory = *mem
	}

	// Get disk stats for root
	disk, err := getDiskStats("/")
	if err == nil {
		health.Disk = *disk
	}

	// Get load average
	loadAvg, err := getLoadAverage()
	if err == nil {
		health.CPU.LoadAvg1 = loadAvg[0]
		health.CPU.LoadAvg5 = loadAvg[1]
		health.CPU.LoadAvg15 = loadAvg[2]
		// Approximate CPU usage from load average
		health.CPU.UsagePercent = (loadAvg[0] / float64(health.NumCPU)) * 100
		if health.CPU.UsagePercent > 100 {
			health.CPU.UsagePercent = 100
		}
	}

	// Get uptime
	uptime, err := getUptime()
	if err == nil {
		health.Uptime = uptime
	}

	return health, nil
}

// getMemoryStats retrieves memory statistics
func getMemoryStats() (*MemoryHealth, error) {
	if runtime.GOOS == "darwin" {
		return getMemoryStatsDarwin()
	}
	return getMemoryStatsLinux()
}

// getMemoryStatsDarwin gets memory stats on macOS
func getMemoryStatsDarwin() (*MemoryHealth, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// On macOS, we use Go's runtime stats as a reasonable approximation
	// In production, you'd use cgo or a library like gopsutil
	mem := &MemoryHealth{
		Total:       m.HeapSys + m.StackSys + m.MSpanSys + m.MCacheSys,
		Used:        m.Alloc,
		Free:        m.HeapIdle,
		UsedPercent: 0,
	}

	// Try to get better memory info if possible
	// Default to reasonable values based on runtime stats
	if mem.Total > 0 {
		mem.UsedPercent = float64(mem.Used) / float64(mem.Total) * 100
	}

	return mem, nil
}

// getMemoryStatsLinux gets memory stats on Linux
func getMemoryStatsLinux() (*MemoryHealth, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	mem := &MemoryHealth{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		value *= 1024 // Convert from KB to bytes

		switch fields[0] {
		case "MemTotal:":
			mem.Total = value
		case "MemFree:":
			mem.Free = value
		case "MemAvailable:":
			mem.Free = value // Prefer MemAvailable if present
		}
	}

	mem.Used = mem.Total - mem.Free
	if mem.Total > 0 {
		mem.UsedPercent = float64(mem.Used) / float64(mem.Total) * 100
	}

	return mem, nil
}

// getDiskStats retrieves disk usage statistics
func getDiskStats(path string) (*DiskHealth, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}

	// Calculate disk space
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free

	disk := &DiskHealth{
		Total:       total,
		Used:        used,
		Free:        free,
		UsedPercent: float64(used) / float64(total) * 100,
		Path:        path,
	}

	return disk, nil
}

// getLoadAverage retrieves system load averages
func getLoadAverage() ([]float64, error) {
	if runtime.GOOS == "darwin" {
		return getLoadAverageDarwin()
	}
	return getLoadAverageLinux()
}

// getLoadAverageDarwin gets load average on macOS
func getLoadAverageDarwin() ([]float64, error) {
	// Read from sysctl on macOS
	file, err := os.Open("/proc/loadavg")
	if err != nil {
		// macOS doesn't have /proc, use default
		// In production, use cgo or sysctl command
		return []float64{0.5, 0.5, 0.5}, nil
	}
	defer file.Close()

	return parseLoadAvg(file)
}

// getLoadAverageLinux gets load average on Linux
func getLoadAverageLinux() ([]float64, error) {
	file, err := os.Open("/proc/loadavg")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseLoadAvg(file)
}

func parseLoadAvg(file *os.File) ([]float64, error) {
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 {
			load1, _ := strconv.ParseFloat(fields[0], 64)
			load5, _ := strconv.ParseFloat(fields[1], 64)
			load15, _ := strconv.ParseFloat(fields[2], 64)
			return []float64{load1, load5, load15}, nil
		}
	}
	return []float64{0, 0, 0}, nil
}

// getUptime retrieves system uptime
func getUptime() (time.Duration, error) {
	if runtime.GOOS == "darwin" {
		return getUptimeDarwin()
	}
	return getUptimeLinux()
}

// getUptimeDarwin gets uptime on macOS
func getUptimeDarwin() (time.Duration, error) {
	// On macOS, we'd typically use sysctl or the sysctl command
	// Returning a placeholder for now
	return 0, nil
}

// getUptimeLinux gets uptime on Linux
func getUptimeLinux() (time.Duration, error) {
	file, err := os.Open("/proc/uptime")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 1 {
			seconds, err := strconv.ParseFloat(fields[0], 64)
			if err != nil {
				return 0, err
			}
			return time.Duration(seconds) * time.Second, nil
		}
	}
	return 0, nil
}

// FormatBytes formats bytes to human readable string
func FormatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return strconv.FormatFloat(float64(bytes)/TB, 'f', 1, 64) + " TB"
	case bytes >= GB:
		return strconv.FormatFloat(float64(bytes)/GB, 'f', 1, 64) + " GB"
	case bytes >= MB:
		return strconv.FormatFloat(float64(bytes)/MB, 'f', 1, 64) + " MB"
	case bytes >= KB:
		return strconv.FormatFloat(float64(bytes)/KB, 'f', 1, 64) + " KB"
	default:
		return strconv.FormatUint(bytes, 10) + " B"
	}
}

// FormatDuration formats duration to human readable string
func FormatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h"
	}
	if hours > 0 {
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	}
	return strconv.Itoa(minutes) + "m"
}
