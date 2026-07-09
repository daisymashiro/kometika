package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	systemInfoTimeout = 5 * time.Second

	topProcessCount = 5
)

// SystemInfo menyimpan informasi sistem
type SystemInfo struct {
	OS          string
	Platform    string
	Kernel      string
	Uptime      time.Duration
	CPUModel    string
	CPUPercent  float64
	CPUTemp     string
	RAMTotalGB  float64
	RAMUsedGB   float64
	RAMPercent  float64
	SwapTotalGB float64
	SwapUsedGB  float64
	SwapPercent float64
	ZRAMDevices []ZRAMDevice
	LocalIP     string
	TopRAMProcs []TopProcess // top 5 berdasarkan RAM
}

type ZRAMDevice struct {
	Name             string
	CompressedMB     float64
	OriginalMB       float64
	CompressionRatio float64
}

// TopProcess menyimpan info proses teratas
type TopProcess struct {
	PID   int32
	Name  string
	RAM   float64 // persen RAM
	MemMB float64 // penggunaan RAM dalam MB
}

// GetSystemInfo mengembalikan laporan sistem dalam bentuk string (siap kirim ke Telegram, dll)
func GetSystemInfo() (string, error) {
	info, err := GetSystemInfoExtended()
	if err != nil {
		// Return partial info jika ada error, tapi tetap error ditampilkan
		log.Printf("[ERROR] GetSystemInfoExtended: %v", err)
	}
	return FormatSystemInfoText(info), err
}

// GetSystemInfoExtended mengembalikan struct SystemInfo yang sudah terisi
func GetSystemInfoExtended() (*SystemInfo, error) {
	// Buat context dengan timeout
	ctx, cancel := context.WithTimeout(context.Background(), systemInfoTimeout)
	defer cancel()

	// Channel untuk menerima hasil atau error
	type result struct {
		info *SystemInfo
		err  error
	}
	resultCh := make(chan result, 1)

	go func() {
		info, err := collectSystemInfo()
		resultCh <- result{info, err}
	}()

	select {
	case res := <-resultCh:
		return res.info, res.err
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout %v saat mengumpulkan info sistem", systemInfoTimeout)
	}
}

// collectSystemInfo melakukan pengumpulan data sebenarnya
func collectSystemInfo() (*SystemInfo, error) {
	var errs []string // kumpulkan error, tapi tetap lanjutkan

	// Host info
	hInfo, err := host.Info()
	if err != nil {
		errs = append(errs, fmt.Sprintf("host info: %v", err))
		hInfo = &host.InfoStat{OS: "unknown", Platform: "unknown", KernelVersion: "unknown"}
	}

	// CPU info
	cpuInfo, err := cpu.Info()
	if err != nil || len(cpuInfo) == 0 {
		errs = append(errs, fmt.Sprintf("cpu info: %v", err))
		cpuInfo = []cpu.InfoStat{{ModelName: "unknown"}}
	}
	cpuModel := cpuInfo[0].ModelName

	// CPU usage (memblokir 1 detik, tapi ini penting)
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		errs = append(errs, fmt.Sprintf("cpu percent: %v", err))
		cpuPercent = []float64{0}
	}
	var cpuUsage float64
	if len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}

	// Memory
	vMem, err := mem.VirtualMemory()
	if err != nil {
		errs = append(errs, fmt.Sprintf("virtual memory: %v", err))
		vMem = &mem.VirtualMemoryStat{Total: 0, Used: 0, UsedPercent: 0}
	}
	swapMem, err := mem.SwapMemory()
	if err != nil {
		errs = append(errs, fmt.Sprintf("swap memory: %v", err))
		swapMem = &mem.SwapMemoryStat{Total: 0, Used: 0, UsedPercent: 0}
	}

	const GB = 1024 * 1024 * 1024
	totalRAM := float64(vMem.Total) / GB
	usedRAM := float64(vMem.Used) / GB
	totalSwap := float64(swapMem.Total) / GB
	usedSwap := float64(swapMem.Used) / GB

	// ZRAM (tidak kritis)
	zramDevices := getZramInfoStruct()

	// Suhu CPU (tidak kritis)
	cpuTemp := getCPUTemp()

	// Top processes (RAM only, untuk performa)
	topRAM := getTopRAMProcesses(topProcessCount)

	// IP lokal
	localIP := GetLocalIP()

	// Jika ada error, log tapi tetap return info parsial
	if len(errs) > 0 {
		log.Printf("[WARN] Beberapa error terjadi: %s", strings.Join(errs, "; "))
	}

	return &SystemInfo{
		OS:          hInfo.OS,
		Platform:    hInfo.Platform,
		Kernel:      hInfo.KernelVersion,
		Uptime:      time.Duration(hInfo.Uptime) * time.Second,
		CPUModel:    cpuModel,
		CPUPercent:  cpuUsage,
		CPUTemp:     cpuTemp,
		RAMTotalGB:  totalRAM,
		RAMUsedGB:   usedRAM,
		RAMPercent:  vMem.UsedPercent,
		SwapTotalGB: totalSwap,
		SwapUsedGB:  usedSwap,
		SwapPercent: swapMem.UsedPercent,
		ZRAMDevices: zramDevices,
		LocalIP:     localIP,
		TopRAMProcs: topRAM,
	}, nil
}

// FormatSystemInfoText menghasilkan string laporan dari struct SystemInfo
func FormatSystemInfoText(info *SystemInfo) string {
	var sb strings.Builder

	// Header
	sb.WriteString("💻 <b>SERVER MONITORING</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")

	// Section 1: System
	sb.WriteString("⚙️ <b>System Info</b>\n")
	sb.WriteString(fmt.Sprintf("├ <b>OS:</b> <code>%s (%s)</code>\n", info.OS, info.Platform))
	sb.WriteString(fmt.Sprintf("├ <b>Kernel:</b> <code>%s</code>\n", info.Kernel))
	sb.WriteString(fmt.Sprintf("└ <b>Uptime:</b> <code>%s</code>\n\n", info.Uptime.String()))

	// Section 2: Hardware
	sb.WriteString("🎛 <b>Hardware Status</b>\n")
	sb.WriteString(fmt.Sprintf("├ <b>CPU:</b> <code>%s</code>\n", info.CPUModel))
	sb.WriteString(fmt.Sprintf("├ <b>Load:</b> <code>%.2f%%</code>\n", info.CPUPercent))
	if info.CPUTemp != "" {
		sb.WriteString(fmt.Sprintf("├ <b>Temp:</b> <code>%s</code>\n", info.CPUTemp))
	}
	sb.WriteString(fmt.Sprintf("├ <b>RAM:</b> <code>%.2f GB / %.2f GB (%.2f%%)</code>\n", info.RAMUsedGB, info.RAMTotalGB, info.RAMPercent))
	sb.WriteString(fmt.Sprintf("└ <b>Swap:</b> <code>%.2f GB / %.2f GB (%.2f%%)</code>\n\n", info.SwapUsedGB, info.SwapTotalGB, info.SwapPercent))

	// Section 3: ZRAM
	if len(info.ZRAMDevices) > 0 {
		sb.WriteString("🧩 <b>ZRAM Status</b>\n")
		for _, z := range info.ZRAMDevices {
			sb.WriteString(fmt.Sprintf("└ <code>%s: %.2f MB (Asli: %.2f MB, Rasio: %.2fx)</code>\n", z.Name, z.CompressedMB, z.OriginalMB, z.CompressionRatio))
		}
		sb.WriteString("\n")
	}

	// Section 4: Top Processes by RAM
	if len(info.TopRAMProcs) > 0 {
		sb.WriteString("💾 <b>Top 5 Proses (RAM)</b>\n")
		for i, p := range info.TopRAMProcs {
			prefix := "├"
			if i == len(info.TopRAMProcs)-1 {
				prefix = "└"
			}
			sb.WriteString(fmt.Sprintf("%s <code>%s (PID %d) - %.2f%% RAM (%.1f MB)</code>\n", prefix, p.Name, p.PID, p.RAM, p.MemMB))
		}
		sb.WriteString("\n")
	}

	// Section 5: Network
	sb.WriteString("🌐 <b>Network</b>\n")
	sb.WriteString(fmt.Sprintf("└ <b>IP Local:</b> <code>%s</code>", info.LocalIP))

	return sb.String()
}

// GetLocalIP mengembalikan alamat IPv4 lokal pertama
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("[ERROR] GetLocalIP: %v", err)
		return "Tidak diketahui"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "Tidak diketahui"
}

// getZramInfoStruct membaca info ZRAM (Linux only)
func getZramInfoStruct() []ZRAMDevice {
	zramDevices, err := filepath.Glob("/sys/block/zram*")
	if err != nil {
		log.Printf("[WARN] glob zram: %v", err)
		return nil
	}
	var devices []ZRAMDevice
	for _, dev := range zramDevices {
		mmStatPath := filepath.Join(dev, "mm_stat")
		data, err := os.ReadFile(mmStatPath)
		if err != nil {
			// ZRAM mungkin tidak aktif, abaikan
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(data)))
		if len(fields) < 2 {
			continue
		}
		origData, err1 := strconv.ParseFloat(fields[0], 64)
		comprData, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 != nil || err2 != nil {
			log.Printf("[WARN] parse mm_stat %s: %v, %v", mmStatPath, err1, err2)
			continue
		}

		var ratio float64
		if comprData > 0 {
			ratio = origData / comprData
		}
		const MB = 1024 * 1024
		devices = append(devices, ZRAMDevice{
			Name:             filepath.Base(dev),
			CompressedMB:     comprData / MB,
			OriginalMB:       origData / MB,
			CompressionRatio: ratio,
		})
	}
	return devices
}

// getCPUTemp membaca suhu CPU dari thermal zone (Linux only)
func getCPUTemp() string {
	zones, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(zones) == 0 {
		return ""
	}
	var temps []float64
	for _, zone := range zones {
		data, err := os.ReadFile(zone)
		if err != nil {
			continue
		}
		tempMilli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			continue
		}
		tempC := float64(tempMilli) / 1000.0
		if tempC > 0 && tempC < 100 {
			temps = append(temps, tempC)
		}
	}
	if len(temps) == 0 {
		return ""
	}
	var sum float64
	for _, t := range temps {
		sum += t
	}
	avg := sum / float64(len(temps))
	return fmt.Sprintf("%.1f°C", avg)
}

// getTopRAMProcesses mengembalikan top N proses berdasarkan penggunaan RAM (paling efisien)
func getTopRAMProcesses(n int) []TopProcess {
	procs, err := process.Processes()
	if err != nil {
		log.Printf("[ERROR] getTopRAMProcesses: %v", err)
		return nil
	}

	type procInfo struct {
		pid    int32
		name   string
		ramPct float64
		memMB  float64
	}
	var allProcs []procInfo

	for _, p := range procs {
		// Nama proses (jika error, pakai "unknown")
		name, err := p.Name()
		if err != nil {
			name = "unknown"
		}

		// Persentase RAM
		memPercent, err := p.MemoryPercent()
		if err != nil {
			memPercent = 0
		}

		// Penggunaan RAM dalam MB
		memInfo, err := p.MemoryInfo()
		var memMB float64
		if err == nil && memInfo != nil {
			memMB = float64(memInfo.RSS) / (1024 * 1024)
		} else {
			memMB = 0
		}

		// Abaikan proses dengan RAM 0 (biasanya kernel thread)
		if memPercent > 0 || memMB > 0 {
			allProcs = append(allProcs, procInfo{
				pid:    p.Pid,
				name:   name,
				ramPct: float64(memPercent),
				memMB:  memMB,
			})
		}
	}

	// Sort descending berdasarkan persen RAM
	sort.Slice(allProcs, func(i, j int) bool {
		return allProcs[i].ramPct > allProcs[j].ramPct
	})

	if len(allProcs) > n {
		allProcs = allProcs[:n]
	}

	result := make([]TopProcess, 0, len(allProcs))
	for _, p := range allProcs {
		result = append(result, TopProcess{
			PID:   p.pid,
			Name:  p.name,
			RAM:   p.ramPct,
			MemMB: p.memMB,
		})
	}
	return result
}
