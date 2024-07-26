package wsl

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// KernelUpdateStatus represents the result of a kernel update check.
type KernelUpdateStatus struct {
	Available      bool   // true if an update is available
	CurrentVersion string // current WSL version
	Message        string // human-readable status message
}

// GPUStatus represents the result of a GPU/CUDA detection check.
// NVIDIA reports three version numbers that are now decoupled:
//   - DriverVersion: Kernel Mode Driver (KMD) — proves GPU is functional.
//     Not what ML runtimes link against; mainly useful for display/gaming.
//   - CUDAVersion: User Mode Driver (UMD) CUDA runtime — this is what
//     PyTorch/TensorFlow/llama.cpp actually use. For LLM inference,
//     this is the signal that matters most.
type GPUStatus struct {
	CUDAVersion   string   // CUDA UMD runtime version (e.g., "13.5") — primary ML signal
	DriverVersion string   // KMD driver version (e.g., "621.14") — GPU functional check
	GPUAvailable  bool     // true if at least one GPU detected and queryable
	GPUs          []string // list of detected GPU names
	Message       string   // human-readable status message
}

// CheckKernelUpdates checks whether a newer WSL kernel version is available.
// Returns the update status and any error encountered during detection.
func CheckKernelUpdates(ctx context.Context) (KernelUpdateStatus, error) {
	out, err := runWslWith(ctx, []string{"--update"})
	if err != nil {
		return KernelUpdateStatus{}, fmt.Errorf("checking kernel updates: %w", err)
	}

	status := KernelUpdateStatus{CurrentVersion: detectWSLVersion(ctx)}

	// Parse the output to determine if an update is available.
	// Expected messages:
	// - "The most recent version of Windows Subsystem for Linux is already installed." (up to date)
	// - "Downloading updates..." or similar indicates updates are being applied
	// - If no specific message, assume up to date unless error occurred

	out = strings.TrimSpace(out)
	if strings.Contains(strings.ToLower(out), "already installed") {
		status.Available = false
		status.Message = "WSL kernel is up to date"
	} else if out == "" || len(out) < 50 {
		// Short or empty output likely means updates were applied (no specific message)
		status.Available = true
		status.Message = "WSL kernel update was applied"
	} else {
		// Assume up to date for any other reasonable output
		status.Available = false
		status.Message = out
	}

	return status, nil
}

// CheckGPUStatus detects NVIDIA GPU and CUDA runtime availability in WSL2.
// Returns the GPU status and any error encountered during detection.
func CheckGPUStatus(ctx context.Context) (GPUStatus, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] CheckGPUStatus called\n")
	status := GPUStatus{
		CUDAVersion:   "unknown",
		DriverVersion: "unknown",
		Message:       "checking...",
	}
	fmt.Fprintf(os.Stderr, "[DEBUG] status initialized, calling runWslWith\n")

	// Try to detect NVIDIA GPU via nvidia-smi (via stdin with full path)
	out, err := runWslWithInput(ctx, []string{"-d", "Ubuntu-Lean", "bash"}, `/usr/lib/wsl/lib/nvidia-smi --query-gpu=name,driver_version --format=csv,noheader`)
	fmt.Printf("[DEBUG] query-gpu: out=%q err=%v\n", out, err)
	if err != nil {
		status.Message = fmt.Sprintf("NVIDIA GPU not detected: %v", err)
		return status, nil
	}

	out = strings.TrimSpace(out)
	fmt.Printf("[DEBUG] trimmed out=%q\n", out)
	if out == "" || strings.Contains(strings.ToLower(out), "error") {
		status.Message = "No NVIDIA GPUs detected"
		return status, nil
	}

	// Parse nvidia-smi output (comma-separated: name,driver_version)
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ",", 2)
		if len(parts) == 2 {
			gpuName := strings.TrimSpace(parts[0])
			driverVer := strings.TrimSpace(parts[1])

			status.GPUAvailable = true
			status.DriverVersion = driverVer
			status.GPUs = append(status.GPUs, gpuName)
		}
	}

	// Get CUDA UMD runtime version from nvidia-smi header.
	// Priority: "CUDA UMD Version" (user-mode driver, what ML runtimes use)
	// over "CUDA Version" (legacy/compatibility line).
	outFull, _ := runWslWith(ctx, []string{"-d", "Ubuntu-Lean", "bash", "-c", `nvidia-smi`})
	if outFull != "" {
		for _, line := range strings.Split(outFull, "\n") {
			if idx := strings.Index(line, "CUDA UMD Version: "); idx != -1 {
				rest := line[idx+len("CUDA UMD Version: "):]
				// Extract version number (digits and dots) until non-version char
				ver := ""
				for _, ch := range rest {
					if (ch >= '0' && ch <= '9') || ch == '.' {
						ver += string(ch)
					} else if ver != "" {
						break
					}
				}
				if ver != "" {
					status.CUDAVersion = ver
					break
				}
			}
			if status.CUDAVersion == "unknown" || status.CUDAVersion == "" {
				if idx := strings.Index(line, "CUDA Version: "); idx != -1 {
					rest := line[idx+len("CUDA Version: "):]
					ver := ""
					for _, ch := range rest {
						if (ch >= '0' && ch <= '9') || ch == '.' {
							ver += string(ch)
						} else if ver != "" {
							break
						}
					}
					if ver != "" {
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
