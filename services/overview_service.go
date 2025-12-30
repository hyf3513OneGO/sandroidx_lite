package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
)

type OverviewService interface {
	GetOverview(ctx context.Context) (*OverviewResponse, error)
}

type OverviewResponse struct {
	ServerTime string         `json:"server_time"`
	Counts     OverviewCounts `json:"counts"`
	System     HostMetrics    `json:"system"`
	API        APIMetrics     `json:"api"`
}

type OverviewCounts struct {
	SandboxesTotal   int64 `json:"sandboxes_total"`
	SandboxesRunning int64 `json:"sandboxes_running"`
	AgentsTotal      int64 `json:"agents_total"`
	AgentsRunning    int64 `json:"agents_running"`
	TemplatesTotal   int64 `json:"templates_total"`
	VolumesTotal     int64 `json:"volumes_total"`
	UsersTotal       int64 `json:"users_total"`
	ApksTotal        int64 `json:"apks_total"`
}

type HostMetrics struct {
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	GoVersion   string `json:"go_version"`
	Goroutines  int    `json:"goroutines"`
	UptimeSec   int64  `json:"uptime_sec"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	CpuPercent  float64 `json:"cpu_percent"`
	MemPercent  float64 `json:"mem_percent"`
	NetRxKBps   float64 `json:"net_rx_kbps"`
	NetTxKBps   float64 `json:"net_tx_kbps"`
	DiskRootPct float64 `json:"disk_root_percent"`
	DiskDataPct float64 `json:"disk_data_percent"`
	DiskDataPath string `json:"disk_data_path"`
}

type overviewService struct {
	sampler *hostSampler
	api     *RequestMetricsCollector
}

func NewOverviewService(apiCollector *RequestMetricsCollector) OverviewService {
	return &overviewService{
		sampler: newHostSampler(),
		api:     apiCollector,
	}
}

func (s *overviewService) GetOverview(ctx context.Context) (*OverviewResponse, error) {
	var counts OverviewCounts

	if err := models.DB.Model(&models.Sandbox{}).Count(&counts.SandboxesTotal).Error; err != nil {
		return nil, fmt.Errorf("统计 sandboxes 失败: %w", err)
	}
	_ = models.DB.Model(&models.Sandbox{}).Where("status = ?", "running").Count(&counts.SandboxesRunning).Error

	if err := models.DB.Model(&models.Agent{}).Count(&counts.AgentsTotal).Error; err != nil {
		return nil, fmt.Errorf("统计 agents 失败: %w", err)
	}
	_ = models.DB.Model(&models.Agent{}).Where("status = ?", "running").Count(&counts.AgentsRunning).Error

	_ = models.DB.Model(&models.Template{}).Count(&counts.TemplatesTotal).Error
	_ = models.DB.Model(&models.Volume{}).Count(&counts.VolumesTotal).Error
	_ = models.DB.Model(&models.User{}).Count(&counts.UsersTotal).Error
	_ = models.DB.Model(&models.Apk{}).Count(&counts.ApksTotal).Error

	metrics := s.sampler.Collect()
	api := APIMetrics{}
	if s.api != nil {
		api = s.api.Snapshot()
	}

	return &OverviewResponse{
		ServerTime: time.Now().Format(time.RFC3339),
		Counts:     counts,
		System:     metrics,
		API:        api,
	}, nil
}

type hostSampler struct {
	mu sync.Mutex

	lastNetAt   time.Time
	lastNetRx   uint64
	lastNetTx   uint64
	lastNetOK   bool
}

func newHostSampler() *hostSampler {
	return &hostSampler{}
}

func (s *hostSampler) Collect() HostMetrics {
	hostname, _ := os.Hostname()

	uptime := readUptimeSec()
	l1, l5, l15 := readLoadAvg()
	cpuPct := sampleCPUPercent(120 * time.Millisecond)
	memPct := readMemPercent()

	netRxKBps, netTxKBps := s.sampleNetKBps()

	rootPct := diskUsedPercent("/")
	dataPct := 0.0
	dataPath := ""
	if configs.AppConfig.Server.DataPath != "" {
		dataPath = configs.AppConfig.Server.DataPath
		dataPct = diskUsedPercent(configs.AppConfig.Server.DataPath)
	}

	return HostMetrics{
		Hostname:    hostname,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		GoVersion:   runtime.Version(),
		Goroutines:  runtime.NumGoroutine(),
		UptimeSec:   uptime,
		Load1:       l1,
		Load5:       l5,
		Load15:      l15,
		CpuPercent:  cpuPct,
		MemPercent:  memPct,
		NetRxKBps:   netRxKBps,
		NetTxKBps:   netTxKBps,
		DiskRootPct: rootPct,
		DiskDataPct: dataPct,
		DiskDataPath: dataPath,
	}
}

func readUptimeSec() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

func readLoadAvg() (float64, float64, float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(fields[0], 64)
	bb, _ := strconv.ParseFloat(fields[1], 64)
	c, _ := strconv.ParseFloat(fields[2], 64)
	return a, bb, c
}

type cpuTimes struct {
	idle  uint64
	total uint64
}

func readCPUTimes() (cpuTimes, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTimes{}, false
	}
	line := sc.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return cpuTimes{}, false
	}
	fields := strings.Fields(line)
	// cpu user nice system idle iowait irq softirq steal guest guest_nice
	if len(fields) < 5 {
		return cpuTimes{}, false
	}
	var nums []uint64
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			v = 0
		}
		nums = append(nums, v)
	}
	var total uint64
	for _, v := range nums {
		total += v
	}
	idle := nums[3]
	if len(nums) > 4 {
		idle += nums[4] // iowait
	}
	return cpuTimes{idle: idle, total: total}, true
}

func sampleCPUPercent(window time.Duration) float64 {
	t1, ok := readCPUTimes()
	if !ok {
		return 0
	}
	time.Sleep(window)
	t2, ok := readCPUTimes()
	if !ok {
		return 0
	}
	dTotal := float64(t2.total - t1.total)
	dIdle := float64(t2.idle - t1.idle)
	if dTotal <= 0 {
		return 0
	}
	used := (dTotal - dIdle) / dTotal * 100.0
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return used
}

func readMemPercent() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var totalKB, availKB uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			totalKB = parseMemInfoKB(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			availKB = parseMemInfoKB(line)
		}
	}
	if totalKB == 0 {
		return 0
	}
	used := float64(totalKB-availKB) / float64(totalKB) * 100.0
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return used
}

func parseMemInfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func (s *hostSampler) sampleNetKBps() (float64, float64) {
	rx, tx, ok := readNetBytes()
	if !ok {
		return 0, 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if !s.lastNetOK {
		s.lastNetAt = now
		s.lastNetRx = rx
		s.lastNetTx = tx
		s.lastNetOK = true
		return 0, 0
	}

	dt := now.Sub(s.lastNetAt).Seconds()
	if dt <= 0 {
		return 0, 0
	}

	drx := float64(rx - s.lastNetRx)
	dtx := float64(tx - s.lastNetTx)
	s.lastNetAt = now
	s.lastNetRx = rx
	s.lastNetTx = tx

	// bytes/s -> KB/s
	return (drx / dt) / 1024.0, (dtx / dt) / 1024.0
}

func readNetBytes() (uint64, uint64, bool) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	var rxTotal, txTotal uint64
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 2 { // skip headers
			continue
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		// 排除 loopback
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		// rx bytes is field[0], tx bytes is field[8]
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rxTotal += rx
		txTotal += tx
	}
	return rxTotal, txTotal, true
}

func diskUsedPercent(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	free := float64(st.Bavail) * float64(st.Bsize)
	if total <= 0 {
		return 0
	}
	used := (total - free) / total * 100.0
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return used
}


