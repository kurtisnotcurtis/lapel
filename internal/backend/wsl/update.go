package wsl

import (
	"context"
	"fmt"
	"strings"
)

// KernelInfo represents a read-only snapshot of installed WSL and Linux kernel
// versions (obtained via `wsl.exe --version`, never mutating the host).
type KernelInfo struct {
	WSLVersion    string // e.g. "2.4.13.0"
	KernelVersion string // e.g. "5.15.167.4-1"
}

// GPUStatus represents the result of a GPU/CUDA detection check.
type GPUStatus struct {
	CUDAVersion   string   // CUDA UMD runtime version (e.g., "13.5") — primary ML signal
	DriverVersion string   // KMD driver version (e.g., "621.14") — GPU functional check
	GPUAvailable  bool     // true if at least one GPU detected and queryable
	GPUs          []string // list of detected GPU names
	Message       string   // human-readable status message
}

// InspectKernelInfo reads the installed WSL and kernel version via
// `wsl.exe --version`. It is strictly read-only and never invokes `--update`.
func InspectKernelInfo(ctx context.Context) (KernelInfo, error) {
	out, err := runWslWith(ctx, []string{"--version"})
	if err != nil {
		return KernelInfo{}, fmt.Errorf("reading WSL version: %w", err)
	}
	return KernelInfo{
		WSLVersion:    parseWSLVersionField(out, "wsl version:"),
		KernelVersion: parseWSLVersionField(out, "kernel version:"),
	}, nil
}

// CheckGPUStatus detects NVIDIA GPU and CUDA runtime availability in WSL2.
func CheckGPUStatus(ctx context.Context, dist string) (GPUStatus, error) {
	if strings.TrimSpace(dist) == "" {
		dist = DefaultDist
	}
	status := GPUStatus{
		CUDAVersion:   "unknown",
		DriverVersion: "unknown",
		Message:       "checking...",
	}

	out, err := runWslWithInput(ctx, []string{"-d", dist, "bash"}, `/usr/lib/wsl/lib/nvidia-smi --query-gpu=name,driver_version --format=csv,noheader`)
	if err != nil {
		status.Message = fmt.Sprintf("NVIDIA GPU not detected: %v", err)
		return status, nil
	}

	out = strings.TrimSpace(out)
	if out == "" || strings.Contains(strings.ToLower(out), "error") {
		status.Message = "No NVIDIA GPUs detected"
		return status, nil
	}

	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, ",", 2)
		if len(parts) == 2 {
			gpuName := strings.TrimSpace(parts[0])
			driverVer := strings.TrimSpace(parts[1])

			status.GPUAvailable = true
			status.DriverVersion = driverVer
			status.GPUs = append(status.GPUs, gpuName)
		}
	}

	outFull, _ := runWslWithInput(ctx, []string{"-d", dist, "bash"}, `/usr/lib/wsl/lib/nvidia-smi`)
	if outFull != "" {
		for _, line := range strings.Split(outFull, "\n") {
			if idx := strings.Index(line, "CUDA UMD Version: "); idx != -1 {
				if ver := extractVersionToken(line[idx+len("CUDA UMD Version: "):]); ver != "" {
					status.CUDAVersion = ver
					break
				}
			}
			if status.CUDAVersion == "unknown" || status.CUDAVersion == "" {
				if idx := strings.Index(line, "CUDA Version: "); idx != -1 {
					if ver := extractVersionToken(line[idx+len("CUDA Version: "):]); ver != "" {
						status.CUDAVersion = ver
					}
				}
			}
		}
	}

	if status.GPUAvailable {
		if status.CUDAVersion != "unknown" && status.CUDAVersion != "" {
			status.Message = fmt.Sprintf("%d GPU(s), CUDA UMD %s, driver %s", len(status.GPUs), status.CUDAVersion, status.DriverVersion)
		} else {
			status.Message = fmt.Sprintf("%d GPU(s) detected (CUDA version unknown), driver %s", len(status.GPUs), status.DriverVersion)
		}
	} else {
		status.Message = "No NVIDIA GPUs detected"
	}

	return status, nil
}

func extractVersionToken(rest string) string {
	var b strings.Builder
	for _, ch := range rest {
		if (ch >= '0' && ch <= '9') || ch == '.' {
			b.WriteRune(ch)
		} else if b.Len() > 0 {
			break
		}
	}
	return b.String()
}
