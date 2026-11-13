# lapel E2E Testing Guide

## Overview

The `lapel` project includes comprehensive end-to-end tests that verify the full provisioning and agent inference stack. These tests run against real WSL distributions and test everything from distro creation to Go compilation inside WSL.

## Running E2E Tests

### Quick Start

```bash
# Run the full e2e test suite (downloads Ubuntu 24.04 rootfs, creates throwaway distro)
TB_E2E=1 go test -run TestE2EFreshImportAndSetup -v -timeout 45m ./internal/backend/wsl/

# Keep the throwaway distro for post-mortem inspection
TB_E2E=1 TB_E2E_KEEP=1 go test -run TestE2EFreshImportAndSetup -v -timeout 45m ./internal/backend/wsl/
```

### What Gets Tested

The e2e test exercises **14 verification steps**:

| # | Category | What's Verified |
|---|----------|-----------------|
| 1 | Distro Creation | `wsl.exe` lists the new distro after provisioning |
| 2 | Default User | Non-root user is set correctly (`whoami` returns expected user) |
| 3 | Passwordless Sudo | `sudo true` works without interaction (critical for automation) |
| 4 | wsl.conf Durability | `/etc/wsl.conf` pins default user and enables systemd |
| 5 | Agent Toolkit Packages | Core packages installed: openssh-server, ripgrep, git-filter-repo, tmux |
| 5b | Command Substitution | `$(...)` survives WSL exec path (regression test for WSL 3.0.3.0) |
| 6 | Full Reboot | Non-root user persists after `wsl --shutdown` and restart |
| 7 | Agent Detection | Agents detectable inside distro: pi, copilot, claude |
| 8 | Inference Providers | LM Studio / Ollama reachability from WSL to Windows host |
| 9 | Go Runtime | `go version` works (we install golang-go) |
| 10 | Python/uv | `uv --version` and python3 availability |
| 11 | Node/fnm | `node --version` and fnm installed |
| 12 | Go Compilation | Compile and run a simple Go program inside WSL |
| 13 | SSH Connectivity | Windows → WSL SSH works (if passwordless sudo configured) |
| 14 | Summary Report | Aggregate stats: agents detected, providers running |

## Test Architecture

### Throwaway Distro Strategy

Tests create a fresh distro (`tb-e2e-<timestamp>`) and unregister it at the end. This ensures:
- **Clean state** — no leftover packages or config from previous runs
- **Real WSL behavior** — tests against actual `wsl.exe`, not mocks
- **Fast cleanup** — `wsl --unregister` removes everything

### Exec Path vs Stdin

The test validates both execution modes:
- **`runWslWith()`** — passes commands via `-c "..."` (argv path)
- **Stdin heredoc** — for commands with complex quoting or `$(...)` substitution

This is critical because WSL 3.0.3.0 mangles command substitutions in argv.

## Agent Detection Testing

The test verifies that agents can be detected inside the provisioned distro:

```go
agentMgr := agent.NewManager()
agents, err := agentMgr.DetectAll(ctx)
// Expected output:
//   pi: ✓ available (version: 1.26.0)
//   copilot: ✗ not found
//   claude: ✗ not found
```

**Note:** Agent binaries (`pi`, `github-copilot-cli`, `claude`) are detected **inside the WSL distribution** via `wsl.exe -d <distro> bash -lc "command -v <binary>"` (falling back to host `exec.LookPath` only when running natively on Linux).

## Inference Provider Testing

The test checks if inference providers are reachable from WSL:

```go
reg := provider.NewRegistry(name) // use our throwaway distro
statuses, err := reg.StatusAll(ctx)
// Expected output:
//   lm-studio: installed=yes running=yes port=1234
//     models: 3 loaded
//   ollama: installed=no running=no port=0
```

### LM Studio Connectivity

LM Studio runs on Windows host, agent runs in WSL. The test verifies:
- Port reachability (default 1234)
- Model listing via API
- Network path from WSL to Windows (NAT gateway IP)

### Ollama Connectivity

Ollama can run as a background service or CLI. The test checks both modes.

## Extending Tests

### Adding New Agent Types

To add an agent to the detection suite:

1. Implement `agent.Agent` interface in `internal/agent/<name>.go`
2. Register it in `agent.NewManager()`
3. Update this documentation

### Adding New Inference Providers

To add a provider:

1. Implement `provider.Manager` interface in `internal/provider/<name>.go`
2. Register it in `provider.NewRegistry()`
3. Add reachability checks to the e2e test

## Opinionated Agent Config for LM Studio

### Host-to-WSL Network Topology

```
┌─────────────────────────────────────┐
│         Windows 11 Host             │
│                                     │
│  ┌──────────┐    ┌───────────────┐  │
│  │LM Studio │    │   lapel.exe   │  │
│  │ :1234     │◄──►│   (WSL)       │  │
│  └──────────┘    └───────────────┘  │
│              ▲                      │
│              │ NAT gateway           │
│         ┌─────────────┐            │
│         │  WSL2 VM    │            │
│         │  (Ubuntu)   │            │
│         └─────────────┘            │
└─────────────────────────────────────┘
```

### Key Configuration Points

1. **LM Studio listens on `0.0.0.0:1234`** — not just localhost, so WSL can reach it
2. **WSL uses NAT gateway IP** — typically `172.x.x.x` or `192.168.x.x` (changes between reboots)
3. **`/etc/resolv.conf` lies** — nameserver points to Windows, not the actual gateway

### Testing from Inside WSL

```bash
# Find the NAT gateway IP
ip route | grep default

# Test LM Studio reachability
curl -s http://<gateway-ip>:1234/v1/models

# Or use the Windows host DNS name (works in most cases)
curl -s http://host.docker.internal:1234/v1/models
```

### Automated Reachability Check

The `provider.LMStudio.Status()` method handles this automatically:

```go
reg := provider.NewRegistry("Ubuntu-Lean")
status, err := reg.FindByName("lm-studio").Status(ctx)
if err != nil {
    // Handle network issues, wrong port, etc.
}
fmt.Printf("LM Studio reachable at %s:%d\n", status.BindAddr, status.Port)
```

## Troubleshooting

### Common Issues

| Issue | Symptom | Fix |
|-------|---------|-----|
| WSL version mismatch | `wsl --version` not found | Update WSL: `winget install Microsoft.WSL` |
| NAT gateway IP changed | LM Studio unreachable after reboot | Use `host.docker.internal` or query `/etc/resolv.conf` |
| LM Studio not listening on all interfaces | Only accessible from Windows, not WSL | Set `--listen 0.0.0.0` in LM Studio settings |
| Passwordless sudo missing | SSH tests fail | Run `sudo visudo` and add user to sudoers with NOPASSWD |

### Debugging Tips

```bash
# Inspect the throwaway distro after test (TB_E2E_KEEP=1)
wsl -d tb-e2e-<timestamp> -- bash

# Check what packages were installed
dpkg -l | grep -E "ripgrep|git-filter-repo|tmux"

# Verify agent binaries inside the WSL distro
wsl -d Ubuntu-Lean -- bash -lc "command -v pi || command -v claude || command -v github-copilot-cli"

# Test LM Studio from WSL manually
curl -s http://host.docker.internal:1234/v1/models | jq .
```

## Future Enhancements

- [ ] Add `lapel doctor` command that runs the e2e checks interactively
- [ ] Support for multiple inference providers simultaneously
- [ ] Test agent launch with actual model inference (slow, optional)
- [ ] Validate jail policies (filesystem scoping, network restrictions)
- [ ] Performance benchmarks: distro boot time, package install time
