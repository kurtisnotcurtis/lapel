# lapel

**Local Agent Protocol Enhancement Layer**

Get local AI working in WSL — properly.

lapel gives you a lean, purpose-built Linux environment optimized for agent workloads, makes sure your inference provider is actually reachable from inside WSL, and keeps your coding agent sandboxed so it doesn't eat your Windows files.

No bloat. No guesswork. Just working local AI.

## The problem

Running a coding agent with a local model on Windows means bridging two worlds that don't agree:

- **LM Studio runs on Windows**, but your agent runs in WSL. Does WSL actually see the server? The NAT gateway IP changes, `resolv.conf` lies to you, mirrored mode breaks in WSL 3.x. You might think your provider is reachable when it isn't.
- **Stock Ubuntu from the Store** is a general-purpose OS living inside Windows — systemd services, background daemons, full PATH with `/mnt/c`, GUI apps. It's eating into the RAM and CPU your inference server is also competing for.
- **Agents delete things.** A coding agent that `rm -rf`s your whole C: drive because you didn't scope it properly is a real fear. Sandboxing isn't optional.

It solves these. Specifically.

## Three pillars

### 1. Provider reachability

Your inference server (LM Studio, Ollama) lives on Windows. Your agent lives in WSL. lapel verifies the connection from the *Linux side*, where it actually matters — not just "is port 1234 open?" but "can I reach the host through NAT, and what's the actual gateway IP right now?"

```bash
lapel doctor          # verify prerequisites
lapel provider reachability   # probe from inside WSL
```

### 2. Lean environment

A purpose-built Ubuntu distro with only what agents need: git, curl, tmux, ripgrep, sshd. No GUI apps. PATH filtered to strip host tool leakage. Less overhead in WSL means more resources for your model — especially important when inference is running on the same machine.

```bash
lapel provision       # build the agent-optimized baseline
lapel env status      # inspect runtime state
```

### 3. Agent sandboxing

Filesystem scoping, network restrictions, process timeouts. Your agent gets read-write access to your projects, deny access to System32, and runs as a non-root user. You can see exactly what's enforced vs. just requested.

```bash
lapel apply           # enforce the jail policy from your manifest
lapel status          # check enforcement is active
```

## The journey

```
doctor → setup → run
              ↘ troubleshoot
```

1. **`doctor`** — Is WSL available? Is my provider reachable? What hardware do I have?
2. **`setup`** — One command to provision the environment, import my workspace, and apply jail policies (coming soon)
3. **`run`** — Launch my agent session with everything configured (coming soon)

For power users who want more control: `init`, `plan`, `apply`, `status` give you declarative management with a YAML manifest and drift detection. But the default experience is guided, not config-first.

## Quick start

```bash
# 1. Check prerequisites
lapel doctor

# 2. Provision a lean WSL environment
lapel provision

# 3. Verify your provider is reachable from inside WSL
lapel provider reachability

# 4. Create a manifest and apply jail policies
lapel init
lapel apply

# 5. Run your agent (when ready)
lapel run
```

## Supported providers

| Provider | How it's detected | Notes |
|----------|------------------|-------|
| **LM Studio** | `lms` CLI + system tray | Preferred; GUI + CLI (`lms server start/stop/status`) |
| **Ollama** | API on `localhost:11434` | Lightweight; background service |

## Configuration

lapel uses a single `lapel.yaml` manifest in your project directory:

```yaml
schemaVersion: 1

distro:
  name: Ubuntu-Lean
  base: ubuntu
  version: "24.04"

packages:
  baseline: []   # use defaults (git, curl, tmux, ripgrep, sshd)
  extras:
    - fd-find
    - tree

jail:
  name: developer
  user: kurt
  filesystem:
    "C:\\Users\\Kurt\\Projects": read-write
    "C:\\Windows\\System32": deny
  network: unrestricted
  processes:
    privileged: false
    timeoutSeconds: 300

workspace:
  path: "."
  importMode: copy   # or "bind" for bind mounts
```

Override the baseline packages, add extras, scope filesystem access, restrict network. The manifest is an escape hatch — the guided setup handles the defaults well.

## Non-goals

- Not a coding-agent harness (you bring your agent: Pi, Claude Code, Copilot)
- Not an inference server (you bring yours: LM Studio, Ollama)
- Not a Docker replacement or general container orchestrator
- Not a hardened security boundary for hostile code
- Not multi-user/tenant (yet)

## Where to look next

- **[`AGENTS.md`](AGENTS.md)** — build/test setup, package layout, technical decisions and gotchas for contributors
- **[`docs/FEATURES.md`](docs/FEATURES.md)** — full feature plan with implementation status
- **`lapel --help`** — all available commands
