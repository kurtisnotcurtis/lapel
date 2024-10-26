package hardware

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// WindowsDetector detects hardware on Windows via wmic/PowerShell.
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
	// Use PowerShell to query Windows for GPU info - reliable from both Windows and WSL
	powershellCmd := exec.CommandContext(ctx, "powershell", "-Command",
		`Get-CimInstance Win32_VideoController | Select-Object Name, @{N="VRAM";E={[math]::Round($_.AdapterRAM / 1GB, 2)}} | Format-List | Out-String`)
	out, err := powershellCmd.Output()
	if err != nil {
		return GPUInfo{}, fmt.Errorf("failed to detect GPU via PowerShell: %w", err)
	}

	// Parse output like:
	// Name               : NVIDIA GeForce RTX 4070 SUPER
	// VRAM               : 12.0
	s := string(out)
	var name, vramStr string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name = strings.TrimSpace(strings.TrimSuffix(parts[1], ","))
			}
		} else if strings.HasPrefix(line, "VRAM") {
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
	// Use wsl.exe to read /proc/meminfo - reliable from Windows/WSL
	xcmd := exec.CommandContext(ctx, "wsl", "--exec", "/bin/sh", "-c", "cat /proc/meminfo | grep -E 'MemTotal:|MemAvailable:'")
	out, err := xcmd.Output()
	if err != nil {
		return RAMInfo{}, fmt.Errorf("failed to detect RAM via wsl.exe: %w (output: %s)", err, string(out))
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
	// Use PowerShell for reliable CPU detection
	cmd := exec.CommandContext(ctx, "powershell", "-Command",
		`(Get-CimInstance Win32_Processor).Name + "," + (Get-CimInstance Win32_Processor).NumberOfCores + "," + (Get-CimInstance Win32_Processor).NumberOfLogicalProcessors`)
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
