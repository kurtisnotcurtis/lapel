<p align="center">
  <img src="assets/lapel-logo.svg" alt="lapel logo" width="128" height="128" />
</p>

<h1 align="center">lapel</h1>

<p align="center">
  <strong>Local Agent Protocol Enhancement Layer</strong><br />
  Bridge your Windows GPU (LM Studio / Ollama) to your coding agents in WSL2 and VS Code — lean, sandboxed, and hardware-tuned.
</p>

`lapel` gives you a purpose-built minimal Linux environment optimized for coding agents, guarantees your Windows inference server is reachable from inside WSL, syncs VRAM-tuned model configs to your CLI agents and IDE extensions, and scopes filesystem access so an autonomous agent doesn't eat your Windows files.

No bloat. No guesswork. Just working local AI.

---

## The problem

Running a coding agent with a local model on Windows means bridging four gaps that break out of the box:

1. **The Network Boundary breaks**: LM Studio and Ollama run on Windows, while CLI agents run in WSL2. The NAT gateway IP changes across reboots, `resolv.conf` lies, and mirrored networking breaks local port bindings in WSL 3.x.
2. **The Resource Budget clashes**: Stock Ubuntu from the Microsoft Store is a general-purpose desktop OS living inside Windows — systemd services, background daemons, `/mnt/c` `PATH` leakage, and GUI apps eating 1–3 GB of RAM that your inference server needs for KV cache.
3. **The Config Matrix is manual & fragile**: Every time you load a new model, you have to manually rewrite JSON endpoints and guess context windows across **Pi, Claude Code, Kilo Code, Continue, or Cline** — hoping your batch size and context cap won't OOM your GPU.
4. **The Blast Radius is unscoped**: Coding agents execute shell commands autonomously. Giving an experimental local model unscoped access to `/mnt/c` is an accident waiting to happen.

`lapel` solves all four in one control plane.

---

## Four pillars

### 1. Cross-boundary provider reachability

Your inference server (LM Studio, Ollama) lives on Windows. Your CLI agent lives in WSL. `lapel` resolves the real Windows host gateway IP from `/proc/net/route` and verifies connectivity from the *Linux side*, where it actually matters — classifying connection-refused vs. timeout vs. bind-address errors.

```bash
lapel doctor                  # verify WSL, GPU/VRAM, and provider reachability
lapel provider reachability   # probe Windows inference endpoint from inside WSL
```

### 2. Lean WSL2 environment (`Ubuntu-Lean`)

A purpose-built minimal distro with only what coding agents need (`git`, `curl`, `tmux`, `ripgrep`, `sshd`). No GUI apps (`guiApplications=false`), no background desktop bloat, and `PATH` filtered to strip `/mnt/c` host tool leakage. Every MB saved in WSL goes back to your model.

```bash
lapel provision               # build the agent-optimized Ubuntu-Lean baseline
lapel env status              # inspect runtime state
```

### 3. Agent sandboxing & honest reporting

Filesystem scoping, process timeouts, and non-root execution (`agent` user). `lapel` distinguishes between controls that are actively **enforced**, **requested**, or **unsupported** on your system — never silently ignoring a rule or overclaiming security.

```bash
lapel apply                   # apply manifest baseline, workspace mount & jail policies
lapel status                  # check live state and drift against baseline
```

### 4. Hardware-aware config sync & tuning profiles

`lapel` detects your GPU VRAM and system RAM, recommends optimal model tiers, context windows, and batch sizes (backed by local heuristics and optional community tuning profiles), and configures your coding agents and VS Code plugins (**Pi, Claude Code, Kilo Code, Continue, Cline**) automatically.

```bash
lapel setup                   # one-command guided setup & hardware profile check
lapel community insights      # view verified tuning profiles for your GPU tier (opt-in)
```

---

## The user journey

```
doctor → setup → run
              ↘ plan / apply / status (declarative manifest)
```

1. **`lapel doctor`** — Check WSL availability, GPU/VRAM specs, model tier recommendations, and provider reachability.
2. **`lapel setup`** — One command to verify providers, provision `Ubuntu-Lean`, mount your workspace, and configure jail policies.
3. **`lapel run`** — Launch your coding agent (`pi`, `claude-code`, `copilot`) inside the sandboxed WSL environment connected to your Windows inference provider.

For declarative environments and teams: `init`, `plan`, `apply`, and `status` manage your setup via a `lapel.yaml` manifest backed by SQLite drift detection (`.lapel/state.db`).

---

## Quick start

```bash
# 1. Check WSL, GPU hardware, model recommendations, and provider reachability
lapel doctor

# 2. Run one-command guided setup for the current project
lapel setup

# 3. Verify your Windows inference provider is reachable from inside WSL
lapel provider reachability

# 4. Launch your coding agent inside the sandboxed environment
lapel run
```

---

## Supported providers & agents

| Category | Supported Targets | How `lapel` Integrates |
|----------|-------------------|------------------------|
| **Inference Providers** | **LM Studio**, **Ollama** | Detects installation & runtime status on Windows; lists loaded models; resolves WSL-to-Windows host gateway IP |
| **CLI Coding Agents** | **Pi**, **Claude Code**, **GitHub Copilot CLI** | Auto-detects installed binaries in WSL; injects resolved provider endpoint, active model, and jail environment |
| **VS Code Plugins** *(in progress)* | **Kilo Code**, **Continue**, **Cline** | Generates plugin config files from live provider models with VRAM-aware context windows |

---

## Declarative configuration (`lapel.yaml`)

For reproducible project setups, `lapel init` scaffolds a `lapel.yaml` manifest in your project directory:

```yaml
apiVersion: lapel.dev/v1
name: my-project

environment:
  distribution: Ubuntu-Lean
  base: ubuntu
  version: "24.04"
  packages:
    - fd-find
    - tree

provider:
  name: local
  type: lm-studio
  baseURL: "http://127.0.0.1:1234"
  connectionTimeout: 8s

agent:
  name: pi
  provider: local

workspace:
  name: my-project
  path: "."
  importMode: bind   # "bind" for live bind mounts or "copy" for isolated rsync copies

jail:
  name: developer
  profile: developer
  user: agent
  filesystem:
    "C:\\Users\\user\\Projects": read-write
    "C:\\Windows\\System32": deny
  network: unrestricted
  processes:
    privileged: false
    timeoutSeconds: 300
```

---

## Privacy & community features

Community tuning insights (`lapel community`) are designed around **local-first privacy**:
- Hardware detection and baseline model recommendations run **100% locally** during `lapel doctor` and `lapel setup`.
- Sharing benchmark or configuration reports with the community backend is **opt-in** and controlled via environment flags (`LAPEL_COMMUNITY_ENABLED`).
- You can point `lapel` at a self-hosted backend at any time via `LAPEL_COMMUNITY_ENDPOINT`.

---

## Non-goals

- **Not a coding-agent harness**: You bring your own agent (Pi, Claude Code, Kilo Code, Continue, Cline, Copilot).
- **Not an inference engine**: You bring your own server (LM Studio, Ollama).
- **Not a heavyweight container orchestrator**: Purpose-built specifically for local AI agent workflows on Windows + WSL2.
- **Not a kernel security boundary for hostile malware**: Designed to prevent accidental agent blast radius (e.g., errant `rm -rf` or `/mnt/c` leakage), with transparent reporting of what is enforced.

---

## Where to look next

- **[`AGENTS.md`](AGENTS.md)** — Build/test setup, package layout, and technical gotchas for contributors
- **[`docs/FEATURES.md`](docs/FEATURES.md)** — Detailed breakdown of the four pillars, CLI commands, and architecture
- **`lapel --help`** — All available CLI commands and flags
