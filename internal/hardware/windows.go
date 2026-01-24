package hardware

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// WindowsDetector detects hardware on Windows via nvidia-smi and PowerShell/CIM.
type WindowsDetector struct{}

func NewWindowsDetector() *WindowsDetector { return &WindowsDetector{} }

func (d *WindowsDetector) Detect(ctx context.Context) (*Info, error) {
	info := &Info{OS: "windows"}

	// Detect GPU
	gpu, err := d.detectGPU(ctx)
	if err != nil {
		fmt.Printf("  ⚠ Could not detect GPU: %s\n", err)
	} else {
		info.GPU = gpu
	}

	// Detect RAM
	ram, err := d.detectRAM(ctx)
	if err != nil {
		return nil, fmt.Errorf("detecting RAM: %w", err)
	}
	info.RAM = ram

	// Detect CPU
	cpu, err := d.detectCPU(ctx)
	if err != nil {
		fmt.Printf("  ⚠ Could not detect CPU: %s\n", err)
	} else {
		info.CPU = cpu
	}

	return info, nil
}

func (d *WindowsDetector) detectGPU(ctx context.Context) (GPUInfo, error) {
	// 1. Prefer nvidia-smi first: Win32_VideoController.AdapterRAM is a 32-bit
	// uint32 that wraps/caps at 4 GB on GPUs with >4 GB VRAM (RTX 4070/4080/4090).
	nvCmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total,compute_cap", "--format=csv,noheader,nounits")
	if out, err := nvCmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
			fields := strings.Split(lines[0], ",")
			if len(fields) >= 2 {
				var vramMB float64
				fmt.Sscanf(strings.TrimSpace(fields[1]), "%f", &vramMB)
				compute := ""
				if len(fields) >= 3 {
					compute = strings.TrimSpace(fields[2])
				}
				return GPUInfo{
					Name:      strings.TrimSpace(fields[0]),
					VRAMGB:    vramMB / 1024.0,
					Compute:   compute,
					Available: true,
				}, nil
			}
		}
	}

	// 2. Fallback to PowerShell Win32_VideoController, sorting by AdapterRAM descending
	// so discrete GPUs are selected ahead of integrated graphics.
	powershellCmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		`Get-CimInstance Win32_VideoController | Sort-Object AdapterRAM -Descending | Select-Object -First 1 Name, @{N="VRAM";E={[math]::Round($_.AdapterRAM / 1GB, 2)}} | Format-List | Out-String`)
	out, err := powershellCmd.Output()
	if err != nil {
		return GPUInfo{}, fmt.Errorf("failed to detect GPU via PowerShell: %w", err)
	}

	s := string(out)
	var name, vramStr string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Name") && name == "" {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name = strings.TrimSpace(strings.TrimSuffix(parts[1], ","))
			}
		} else if strings.HasPrefix(line, "VRAM") && vramStr == "" {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				vramStr = strings.TrimSpace(strings.TrimSuffix(parts[1], ","))
			}
		}
	}

	if name == "" || vramStr == "" {
		return GPUInfo{}, fmt.Errorf("could not parse GPU info: %s", s)
	}

	var vramGB float64
	fmt.Sscanf(vramStr, "%f", &vramGB)

	return GPUInfo{
		Name:      name,
		VRAMGB:    vramGB,
		Available: true,
	}, nil
}

func (d *WindowsDetector) detectRAM(ctx context.Context) (RAMInfo, error) {
	// 1. Query physical host memory via Win32_OperatingSystem (values are in KB)
	// so .wslconfig memory caps (e.g. memory=4GB) don't mask the host's real RAM.
	psCmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		`$os = Get-CimInstance Win32_OperatingSystem; "$($os.TotalVisibleMemorySize),$($os.FreePhysicalMemory)"`)
	if out, err := psCmd.Output(); err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), ",")
		if len(parts) == 2 {
			var totalKB, availKB int64
			fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &totalKB)
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &availKB)
			if totalKB > 0 {
				totalGB := float64(totalKB) / (1024 * 1024)
				availGB := float64(availKB) / (1024 * 1024)
				usedPercent := ((totalGB - availGB) / totalGB) * 100
				return RAMInfo{
					TotalGB:     totalGB,
					AvailableGB: availGB,
					UsedPercent: usedPercent,
				}, nil
			}
		}
	}

	// 2. Fallback to WSL /proc/meminfo if PowerShell CIM query fails.
	xcmd := exec.CommandContext(ctx, "wsl", "--exec", "/bin/sh", "-c", "cat /proc/meminfo | grep -E 'MemTotal:|MemAvailable:'")
	out, err := xcmd.Output()
	if err != nil {
		return RAMInfo{}, fmt.Errorf("failed to detect RAM: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var totalKB, availKB int64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(strings.Fields(line)[1], "%d", &totalKB)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(strings.Fields(line)[1], "%d", &availKB)
		}
	}

	if totalKB == 0 {
		return RAMInfo{}, fmt.Errorf("could not parse /proc/meminfo from wsl.exe")
	}

	totalGB := float64(totalKB) / (1024 * 1024)
	availGB := float64(availKB) / (1024 * 1024)
	usedPercent := ((totalGB - availGB) / totalGB) * 100

	return RAMInfo{
		TotalGB:     totalGB,
		AvailableGB: availGB,
		UsedPercent: usedPercent,
	}, nil
}

func (d *WindowsDetector) detectCPU(ctx context.Context) (CPUInfo, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		`$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1; "$($cpu.Name),$($cpu.NumberOfCores),$($cpu.NumberOfLogicalProcessors)"`)
	out, err := cmd.Output()
	if err != nil {
		return CPUInfo{}, fmt.Errorf("running powershell: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) < 3 {
		return CPUInfo{}, fmt.Errorf("no CPU info found")
	}

	name := strings.TrimSpace(parts[0])
	var cores, threads int
	fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &cores)
	fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &threads)

	return CPUInfo{
		Name:    name,
		Cores:   cores,
		Threads: threads,
	}, nil
}
