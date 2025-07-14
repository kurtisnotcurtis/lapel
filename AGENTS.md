# AGENTS.md — lapel contribution guide

AI coding agents: read this before editing code, running builds/tests, or
adding features. It captures the current implementation state, the build/test
setup that actually works on this machine, package conventions, and the
hard-won technical decisions from de-risking so you don't relearn them by pain.

## Product context

lapel (Local Agent Protocol Enhancement Layer) solves three specific problems for power users running coding agents with local AI:

1. **Provider reachability** — LM Studio/Ollama runs on Windows, agent runs in WSL. Verifying the connection from the Linux side (where it actually matters) is non-trivial: NAT gateway IP changes, `resolv.conf` lies, mirrored mode breaks.
2. **Lean environment** — Stock Ubuntu from the Store is bloated. A purpose-built minimal distro frees RAM/CPU for inference, especially when both run on the same machine.
3. **Agent sandboxing** — Filesystem scoping, network restrictions, process timeouts. Trust builder so agents can work without fear of eating your Windows files.

**Scope priorities:** Provider reachability is #1 (nothing works without it). Lean distro is the differentiator. Jail/sandboxing is the trust layer. Everything else (declarative state store, transactional rollback, rsync vs bind mount modes) supports these or is deferred.

**User journey:** `doctor` → `setup` → `run`. The YAML manifest and `plan`/`apply` are power-user escape hatches, not the default experience.

## Environment setup (this machine)

- Host: Windows 11; Linux runtime: WSL2 (`wsl.exe`), distro probed as
  `Ubuntu-Lean`.
- Go is installed manually under `/usr/local/go`. Before any build/test/set, set
  the PATH so both `go` and any locally built tools are found:

```sh
export PATH=$(go env GOPATH)/bin:/usr/local/go/bin:$PATH
```

- Build: `./build.sh build` or `make build` — produces `bin/lapel.exe`.
  The binary includes version info from git (commit hash, date).
- Vet: `go vet ./...` — should be clean.
- Test: `./build.sh test` or `make test` runs the live suites (`internal/backend/wsl`,
  `internal/provider`). Tests hit real WSL, so they require a working `wsl.exe`.
- Doctor: `./build.sh doctor` builds and runs `lapel doctor` to verify
  WSL availability and version detection.
- If a build reports a stale error despite correct source on disk (WSL/NTFS or
  build-cache poisoning), run `go clean -cache` and rebuild before concluding the
  code is wrong.

## Module & tooling notes

- Module path: `lapel`; `go.mod` declares `go 1.23`. The active toolchain
  here is **go1.27.1**. See the critical gotcha below — it affects both files
  that spread a variadic result into another multi-argument call.

## Critical technical gotchas (do not regress these)

1. **go1.27 nested-variadic type-check bug.** Spreading a variadic parameter
   *nested inside* another multi-argument call fails to type-check, e.g.
   `outer(inner(param)...)`. Workaround: assign the spread result to a local
   variable first, then pass it — `wslArgs(...)`, assigned inline before use.
   Applied in `internal/backend/wsl/detect.go` and `execute.go`. If you hit an
   "cannot use … as []string" error on such a call, this is almost certainly it.

2. **`wsl.exe` does not forward positional parameters.** Run commands via
   `bash -c <script>` with per-arg shell quoting (`shellQuote`) instead of
   trying to pass args positionally. The script wrapper also centralises working
   directory, environment injection, process tracking, and exit-code propagation
   (see `execute.go`'s `buildScript`).

3. **Sanitize `wsl.exe` output.** Strip UTF-16 NUL padding / control bytes before
   parsing distro lists or `/etc/os-release`, otherwise config values leak null
   bytes into later commands (`sanitize` in `detect.go`; normalise paths in the
   CLI before execution).

4. **Session teardown relies on WSL's session lifetime.** Killing the launched
   `wsl.exe` handle reliably tears down the associated guest process tree (probe:
   0 leftover processes), because WSL ties guest-session lifetime to that
   connection. Lifecycle still records setsid leaders / PGIDs and kills the guest
   process group as defense-in-depth (`lifecycle.go`, `Process.Stop`).
   **Important:** `Stop()` scans `/tmp/lapel-*.pg` files in the guest to find
   active sessions, reads their PIDs, and kills the process groups. Don't rely on
   the detached-shell probe alone — always verify with a Go test that launches real
   children, stops them, and asserts no orphans remain.

5. **Provider host resolution.** Resolve the Windows host IP from the WSL default
   route in `/proc/net/route` (field 3 of the `dest==00000000` row), decoded
   little-endian hex (e.g. `0104A8C0` → `192.168.4.1`). Fallbacks: `\\wsl$`, or
   the resolved nameserver. Do **not** rely on WSL's `resolv.conf` nameserver for
   host resolution — it is the WSL resolver, not a real upstream. Distinguish
   connection-refused vs timeout in diagnostics (networking vs auth vs model
   errors). See `internal/provider/connectivity.go`. The NAT gateway IP is
   **dynamic per network** (this machine moved from 192.168.4.x to 172.29.x after
   a `.wslconfig` change) — never hardcode it, in code or tests.

6. **`wsl.exe` global vs distro-scoped flags.** `--unregister`, `--list`/`-l`
   are *global* commands: `wsl.exe --unregister <name>`. The form
   `-d <name> --unregister` is invalid and fails with "no such instance"
   (see `runWslGlobal` in `detect.go`). `--set-default-user` is a `--manage`
   option, not a top-level flag: `wsl.exe --manage <name> --set-default-user
   <user>` (the top-level form fails with "Invalid command line argument" on
   3.0.3.0). `wsl.exe -l -v` output is column-formatted
   with a `*` prefix on the default distro — parse it column-aware, not by naive
   whitespace split (`parseDistroNames`).

7. **dpkg-query `${Status}` expands to four fields.** `${Status}` yields
   "install ok installed"; the single-field form is `${db:Status-Status}`.
   Comparing the 4-word expansion against `installed` marks every package missing
   on every run (see `parseInstalledPackages`).

8. **Process-group syscalls are Unix-only.** `Setpgid`/`Getpgid`/`syscall.Kill`
   don't exist on Windows, so they live behind build tags in
   `processgroup_unix.go` / `processgroup_windows.go`; the repo must build under
   both GOOSes (`go build ./...` on Windows and `GOOS=linux go vet ./...`).
   The guest-side kill is the definitive teardown; the host-side PGID kill is
   belt-and-braces only.

## Package layout & conventions

```
cmd/lapel/
  main.go              Root CLI: doctor, env status, provider detect/status/reachability/model-list,
                       session smoke, config, provision, check-localhost, plan, init, apply, status
  apply.go             apply, jail-controls subcommands
  init.go              init command
  plan.go              plan command
internal/config/       Manifest loading, validation, utilities
internal/domain/
  model.go             Provider/Agent/Environment/Workspace/Jail/Session + status/exec types
  manifest.go          Domain-level manifest types
internal/backend/
  backend.go           ExecutionBackend interface + wsl registration
  wsl/
    detect.go          Distro discovery, /etc/os-release parsing, Detect/Inspect, runWslGlobal
    execute.go         Structured exec via bash -c <script>, Launch/Process lifecycle, root escalation
    lifecycle.go       Stop / Destroy (session teardown)
    provision.go       ImportMinimalDistro + first-boot setup (user, sudo, wsl.conf, toolkit)
    ssh.go             EnsureSSHAccess / VerifySSH (sshd hardening, authorized_keys)
    config.go          InspectConfig: read-only .wslconfig assessment (caps + warnings)
    update.go          Version/update utilities
    processgroup_{unix,windows}.go  Build-tagged process-group syscalls
internal/jail/
  enforcer.go          Jail enforcement (sandboxing, PATH filtering, env scoping)
internal/manifest/     YAML manifest parser
internal/provider/
  manager.go           Manager interface for inference provider lifecycle
  lmstudio.go          LM Studio adapter (lms CLI server commands)
  ollama.go            Ollama adapter (API-based detection, background service)
  registry.go          Registry to manage multiple providers
  connectivity.go      Host-reachability checks + error classification (run from WSL)
  errors.go            Custom error types: ErrNotInstalled, ErrServerDown, ErrDetectFailed
internal/reconcile/
  engine.go            Reconcile engine with drift detection and transactional apply/rollback
  probes.go            Health check probes
  reconcile.go         Core reconciliation logic
internal/secrets/      Credential management + secret storage
internal/state/        SQLite state store (packages, manifests, transactions)
internal/workspace/
  importer.go          Workspace import (rsync copy mode)
  path.go              Win/Lin path resolution utilities
```

Conventions:

- Keep interfaces small. Only introduce an interface where a real boundary
  exists — execution backends and agent adapters get interfaces; individual
  functions do not.
- Never build shell commands by concatenating arbitrary manifest/paths values;
  use structured arg arrays + the controlled Linux-side wrapper, with quoting.
- Secrets (provider API keys) stay out of manifests and diagnostic logs; provider
  endpoint resolution lives in the provider manager, not every harness adapter.
- Report enforcement honestly: distinguish *requested* vs *enforced* vs
  *unsupported* vs *needs-config* controls — never silently ignore or overclaim.
- **Scope discipline:** Provider reachability and lean environment are core. Jail/sandboxing is the trust layer. Declarative state, transactional rollback, and advanced workspace import modes support power users but aren't the primary narrative. Don't let new features expand scope beyond the three pillars without explicit justification.

## WSL environment facts that affect design

- The probed distro is `Ubuntu-Lean` (Ubuntu 26.10), **not** the pinned
  Ubuntu-24.04 in the spec. Treat it as an existing environment; provision a fresh
  pinned root filesystem with checksum verification for reproducible v1, and do
  not silently upgrade base images or runtimes.
- The distro is minimal: no `ip`/net-tools, no `python3`; only `curl` (and git-ish
  tooling) present. Bootstrap/installation must install declared packages via apt
  rather than assume them.
- **Why minimal matters.** Stock Ubuntu from the Store runs systemd services,
  background daemons, mounts `/mnt/c` in PATH, loads GUI apps. When your inference
  server (LM Studio/Ollama) is also running on this machine, every MB of RAM and
  percent of CPU that WSL wastes is taken from your model. The lean baseline
  (git, curl, tmux, ripgrep, sshd) is a feature, not a limitation — it's how we
  give your agent more resources to work with.
- A non-root user exists and is in the sudo group, but **sudo requires interactive
  auth** — there is no passwordless sudo by default. Automated provisioning of the
  dedicated `agent` user must add a passwordless-sudoers rule (or otherwise run
  privileged steps) to achieve unattended `apply`.
- Host `.wslconfig` (this machine): NAT networking is **intentional** — mirrored
  mode was tested on WSL 3.0.3.0 and Windows could not reach any Linux-bound port
  in it, so keep NAT for `ssh kurt@localhost` from Windows. Also set:
  `memory=4GB`, `processors=4`, `swap=1GB`, `guiApplications=false`,
  `debugConsole=false`, `dnsTunneling=true`. `InspectConfig` warns when
  `networkingMode=mirrored` or `generateResolvConf=false` reappear.
- `/etc/wsl.conf` in Ubuntu-Lean: systemd enabled, `[user] default=kurt`, no static
  `resolv.conf` (WSL-managed DNS). Fresh imports get the same via
  `setupFreshDistro` — write the durable wsl.conf *before* switching the default
  user so the first login is already correct.


