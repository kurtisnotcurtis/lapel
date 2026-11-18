package hardware

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LinuxDetector detects hardware on Linux (WSL).
type LinuxDetector struct{}

func NewLinuxDetector() *LinuxDetector { return &LinuxDetector{} }

func (d *LinuxDetector) Detect(ctx context.Context) (*Info, error) {
	info := &Info{OS: "linux"}

	// Detect GPU via lspci or nvidia-smi
	gpu, err := d.detectGPU(ctx)
	if err != nil {
		fmt.Printf("  ⚠ Could not detect GPU: %s\n", err)
	} else {
		info.GPU = gpu
	}

	// Detect RAM from /proc/meminfo
	ram, err := d.detectRAM()
	if err != nil {
		return nil, fmt.Errorf("detecting RAM: %w", err)
	}
	info.RAM = ram

	// Detect CPU from /proc/cpuinfo
	cpu, err := d.detectCPU()
	if err != nil {
		fmt.Printf("  ⚠ Could not detect CPU: %s\n", err)
	} else {
		info.CPU = cpu
	}

	return info, nil
}

func (d *LinuxDetector) detectGPU(ctx context.Context) (GPUInfo, error) {
	// Try nvidia-smi first for NVIDIA GPUs
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total,memory.used,compute_cap", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			fields := strings.Split(lines[0], ",")
			if len(fields) >= 2 {
				var vramMB, usedMB float64
				fmt.Sscanf(strings.TrimSpace(fields[1]), "%f", &vramMB)
				if len(fields) >= 3 {
					fmt.Sscanf(strings.TrimSpace(fields[2]), "%f", &usedMB)
				}
				compute := ""
				if len(fields) >= 4 {
					compute = strings.TrimSpace(fields[3])
				}
				return GPUInfo{
					Name:       strings.TrimSpace(fields[0]),
					VRAMGB:     vramMB / 1024.0,
					VRAMUsedGB: usedMB / 1024.0,
					Compute:    compute,
					Available:  true,
				}, nil
			}
		}
	}

	// Fallback to lspci for AMD/Intel GPUs
	cmd = exec.CommandContext(ctx, "lspci", "-nn | grep -i vga")
	out, err = cmd.Output()
	if err != nil {
		return GPUInfo{}, fmt.Errorf("no GPU detected (tried nvidia-smi and lspci)")
	}

	name := strings.TrimSpace(string(out))
	return GPUInfo{
		Name:      name,
		VRAMGB:    0, // Unknown without nvidia-smi
		Available: true,
	}, nil
}

func (d *LinuxDetector) detectRAM() (RAMInfo, error) {
	// Try /proc/meminfo first
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return RAMInfo{}, fmt.Errorf("opening /proc/meminfo: %w", err)
	}
	defer file.Close()

	var totalKB, availKB int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(strings.Fields(line)[1], "%d", &totalKB)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(strings.Fields(line)[1], "%d", &availKB)
		}
	}

	if totalKB == 0 {
		return RAMInfo{}, fmt.Errorf("could not parse /proc/meminfo")
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

func (d *LinuxDetector) detectCPU() (CPUInfo, error) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return CPUInfo{}, fmt.Errorf("opening /proc/cpuinfo: %w", err)
	}
	defer file.Close()

	var name string
	cores := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") && name == "" {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name = strings.TrimSpace(parts[1])
			}
		} else if strings.HasPrefix(line, "processor") {
			cores++
		}
	}

	return CPUInfo{
		Name:    name,
		Cores:   cores,
		Threads: cores, // Assume 1 thread per core for simplicity
	}, nil
}
