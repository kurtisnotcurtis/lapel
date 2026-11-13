# AGENTS.md — lapel contribution guide

AI coding agents: read this before editing code, running builds/tests, or
adding features. It captures the current implementation state, the build/test
setup that works on this machine, package conventions, and the hard-won
technical decisions from de-risking so you don't relearn them by pain.

## Product context

**lapel** is the WSL2 + Local GPU + Coding Agent Control Plane for Windows developers. It solves four tightly coupled problems:

1. **WSL-to-Host Provider Reachability & Hardware Diagnostics** — LM Studio/Ollama runs on the Windows host GPU; coding agents run inside WSL2. `lapel` resolves the dynamic WSL NAT gateway IP from `/proc/net/route`, classifies firewall/bind-address errors, and inspects GPU VRAM via `nvidia-smi`.
2. **Lean, Jailed WSL2 Execution (`Ubuntu-Lean`)** — Stock Ubuntu wastes 2–4 GB of RAM needed for model weights and leaks Windows `/mnt/c` binaries into `PATH`. `lapel` provisions a minimal Ubuntu baseline (`git`, `curl`, `tmux`, `ripgrep`, `openssh-server`) and enforces a clean Linux `PATH`, filesystem scoping, and process-group teardown.
3. **Zero-Touch Agent & VS Code Plugin Wiring** — Detects coding agents (`pi`, `claude-code`, `copilot`) inside WSL and generates VRAM-tuned model configurations for VS Code extensions (**Kilo Code**, **Continue**, **Cline**) via `lapel apply --plugin=...` and `lapel plugin sync`.
4. **Opt-In Hardware Benchmarking & Tuning Insights** — Measures real `tok/s` throughput via `lapel benchmark` and (when `LAPEL_COMMUNITY_ENABLED=true`) shares/queries anonymized hardware-hash tuning profiles via `lapel community`.

**User journey:** `lapel doctor` → `lapel setup` → `lapel run` (or `lapel apply --plugin=kilo-code`), backed by a single declarative manifest schema (`lapel.yaml`, `apiVersion: lapel.dev/v1`).

## Environment setup (this machine)

- Host: Windows 11; Linux runtime: WSL2 (`wsl.exe`), distro probed as `Ubuntu-Lean`.
- Build: `./build.sh build` or `make build` — produces `bin/lapel.exe`.
- Vet: `go vet ./...` — must be clean.
- Unit tests (no WSL required):
  ```sh
  go test ./community/... ./internal/config/... ./internal/features/... ./internal/jail/... ./internal/plugins/... ./internal/reconcile/... ./internal/state/... ./internal/workspace/... ./cmd/lapel/...
  ```
- Live WSL suites: `./build.sh test` or `make test` runs all tests including live WSL tests (`internal/backend/wsl`, `internal/provider`), which require a working `wsl.exe`.

## Critical technical gotchas (do not regress these)

1. **go1.27 nested-variadic type-check bug.** Spreading a variadic parameter *nested inside* another multi-argument call fails to type-check, e.g. `outer(inner(param)...)`. Workaround: assign the spread result to a local variable first, then pass it (`wslArgs(...)` assigned inline before use in `internal/backend/wsl/detect.go` and `execute.go`).

2. **`wsl.exe` does not forward positional parameters.** Run commands via `bash -c <script>` with per-arg shell quoting (`shellQuote`) instead of trying to pass args positionally. `buildScript` in `internal/backend/wsl/execute.go` centralizes working directory, environment injection, process tracking, and exit-code propagation.

3. **Sanitize `wsl.exe` output.** Strip UTF-16 NUL padding / control bytes before parsing distro lists or `/etc/os-release`, otherwise config values leak null bytes into later commands (`sanitize` in `detect.go`).

4. **Session teardown relies on WSL's session lifetime + guest PGID tracking.** Killing the launched `wsl.exe` handle tears down the associated guest process tree, and `Process.Stop()` also scans `/tmp/lapel-*.pg` files in the guest and kills the process groups (`lifecycle.go`).

5. **Provider host resolution.** Resolve the Windows host IP from the WSL default route in `/proc/net/route` (field 3 of the `dest==00000000` row), decoded little-endian hex (e.g. `0104A8C0` → `192.168.4.1`). Do **not** rely on WSL's `resolv.conf` nameserver for host resolution. See `internal/provider/connectivity.go`. The NAT gateway IP is **dynamic per network** — never hardcode it.

6. **`wsl.exe` global vs distro-scoped flags.** `--unregister`, `--list`/`-l` are *global* commands (`wsl.exe --unregister <name>`). `--set-default-user` is a `--manage` option (`wsl.exe --manage <name> --set-default-user <user>`). `wsl.exe -l -v` output is column-formatted with a `*` prefix on the default distro — parse it column-aware (`parseDistroNames`).

7. **dpkg-query `${Status}` expands to four fields.** `${Status}` yields `"install ok installed"`; the single-field form is `${db:Status-Status}` (see `parseInstalledPackages`).

8. **Process-group syscalls are Unix-only.** `Setpgid`/`Getpgid`/`syscall.Kill` live behind build tags in `processgroup_unix.go` / `processgroup_windows.go`; the repo must build under both `GOOS=windows` and `GOOS=linux`.

## Package layout & conventions

```
cmd/lapel/
  main.go              Root CLI: doctor, env status, provider, config, provision, plan
  setup.go             One-command guided setup (manifest-aware)
  run.go               Sandboxed WSL agent session launcher (manifest-aware)
  init.go              Starter lapel.yaml manifest generator
  apply.go             Desired-state apply (provision, workspace import, plugin sync, state store)
  plan.go              Diff display helpers
  plugin.go            VS Code plugin config commands (plugin list, plugin sync)
  benchmark.go         Automated provider throughput benchmark (lapel benchmark)
  community.go         Opt-in community commands (status, benchmark, share, insights)
community/
  model.go             Exported shared schema (Report, ModelPerf, AgentPerf, Recommendation, HardwareProfile)
internal/agent/        WSL-native coding agent detection & env wiring (pi, claude-code, copilot)
internal/backend/wsl/
  detect.go            Distro discovery, /etc/os-release parsing, Detect/Inspect, runWslGlobal
  execute.go           Structured exec via bash -c <script>, Launch/Process lifecycle, root escalation
  lifecycle.go         Stop / Destroy (session teardown)
  provision.go         ImportMinimalDistro + first-boot setup (user, sudo, wsl.conf, toolkit)
  ssh.go               EnsureSSHAccess / VerifySSH (sshd hardening, authorized_keys)
  config.go            InspectConfig: read-only .wslconfig assessment (caps + warnings)
  update.go            Kernel & GPU/CUDA inspection utilities
  processgroup_{unix,windows}.go  Build-tagged process-group syscalls
internal/config/       Single lapel.yaml manifest loader & validator (apiVersion: lapel.dev/v1)
internal/domain/       Core domain types (Environment, Provider, Agent, Workspace, Jail, ExecRequest)
internal/features/     Opt-in feature flag evaluation (LAPEL_COMMUNITY_ENABLED, default false)
internal/hardware/     GPU/VRAM, RAM, CPU detection & model tier recommender
internal/jail/         Execution policy enforcement (clean PATH, env scoping, timeout wrapper)
internal/plugins/      VRAM-aware VS Code plugin config generators (kilo-code, continue, cline)
internal/provider/     LM Studio & Ollama discovery, native HTTP model listing, WSL connectivity
internal/reconcile/    Live vs. desired-state reconciliation (Diff & probes)
internal/state/        SQLite desired-state baseline store (.lapel/state.db)
internal/workspace/    Workspace import (bind mount & rsync copy modes) + Win/Linux path utilities
```

Conventions:

- Keep interfaces small. Only introduce an interface where a real boundary exists — execution backends, providers, plugins, and agent adapters get interfaces; individual functions do not.
- Never build shell commands by concatenating arbitrary manifest/path values; use structured arg arrays + `shellQuote`.
- Report enforcement honestly: distinguish *enforced* vs *requested* vs *needs-config* vs *unsupported* controls — never silently ignore or overclaim.
