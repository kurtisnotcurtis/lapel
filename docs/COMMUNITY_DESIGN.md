# Lapel Community Hardware Intelligence Design

## Vision

Build an **opt-in, empirical hardware tuning index** for local AI coding rigs that makes `lapel`:
- **Immediately useful** — Answers *"Which quant, batch size, and context window actually run best on my GPU + RAM tier without OOMing when WSL2 + VS Code are open?"*
- **Privacy-first by default** — Strictly opt-in (`LAPEL_COMMUNITY_ENABLED=false` by default), with zero background daemons, zero prompt/code capture, and deterministic SHA-256 hardware hashing.
- **Compounding** — Every verified benchmark shared via `lapel benchmark --share` improves recommendations for other developers on the same GPU/VRAM tier.

---

## Core Architecture

### 1. Hardware Detection (`internal/hardware/`)

Detects the host GPU, VRAM, system RAM, and CPU (`nvidia-smi` + OS memory/CPU inspection) and normalizes them into a `community.HardwareProfile`:

```go
type HardwareProfile struct {
    OS         string    `json:"os"`
    RAM        int64     `json:"ram_gb"`
    GPUs       []GPUInfo `json:"gpus"`
    CPUModel   string    `json:"cpu_model"`
    CPUCores   int       `json:"cpu_cores"`
    DetectedAt time.Time `json:"detected_at"`
}
```

### 2. Two-Part Deterministic Hardware Hash (`community/model.go`)

`HardwareProfile.Hash()` computes a 16-character hex identifier:
- **First 8 hex chars (Tier Prefix)**: SHA-256 of normalized `GPU name + VRAM GB` (e.g. `nvidia rtx 4090:24`). Allows the similarity engine to match all users with the same GPU/VRAM tier even if their CPU or system RAM differs slightly.
- **Second 8 hex chars (Full Rig Suffix)**: SHA-256 of normalized `OS + RAM bucket + GPU + CPU cores`. Allows exact-rig matching when sufficient samples exist.

### 3. Shared Data Contract (`community/model.go`)

Exported at the top level of the `lapel` module so both the public CLI (`lapel`) and the community backend (`lapel-product`) compile against the exact same structs:

```go
type Report struct {
    ID           string      `json:"id,omitempty"`
    UserID       string      `json:"user_id,omitempty"`
    HardwareHash string      `json:"hardware_hash"`
    Models       []ModelPerf `json:"models"`
    Agents       []AgentPerf `json:"agents,omitempty"`
    ConfigHash   string      `json:"config_hash,omitempty"`
    ReportedAt   time.Time   `json:"reported_at"`
}

type ModelPerf struct {
    Name        string  `json:"name"`
    Provider    string  `json:"provider"`
    VRAMUsed    float64 `json:"vram_used_gb"`
    BatchSize   int     `json:"batch_size"`
    LoadTimeSec float64 `json:"load_time_sec,omitempty"`
    TokenRate   float64 `json:"token_rate"`
    UserRating  int     `json:"user_rating,omitempty"`
    Notes       string  `json:"notes,omitempty"`
}

type Recommendation struct {
    Type        string  `json:"type"`         // "model", "agent", "config"
    Title       string  `json:"title"`        // e.g., "Ornith 35B (batch size 16)"
    Description string  `json:"description"`  // Avg tok/s and VRAM usage
    Confidence  float64 `json:"confidence"`   // 0.0 - 1.0
    SourceCount int     `json:"source_count"` // Number of corroborating reports
    MatchReason string  `json:"match_reason"` // Exact rig match vs. GPU/VRAM tier match
}
```

---

## On-Demand CLI Workflows (No Background Daemons)

### 1. `lapel benchmark` (or `lapel community benchmark`)
Runs a standardized coding prompt against the active local OpenAI-compatible provider (LM Studio or Ollama), measures real token throughput (`tok/s`), renders a shareable **Lapel Rig Benchmark Card**, and optionally uploads the verified report when `--share` is passed:

```bash
# Preview the measured report locally without network calls
$ lapel benchmark --dry-run

# Run and publish to the community index (requires opt-in flag)
$ LAPEL_COMMUNITY_ENABLED=true lapel benchmark --share
```

### 2. `lapel community status`
Displays the local machine's deterministic hardware hash, GPU/VRAM tier prefix, and current opt-in state.

### 3. `lapel community insights`
Queries `GET /api/v1/recommendations?hash=<hash>&limit=5` for verified tuning recommendations from matching hardware (or falls back to local deterministic VRAM heuristics when offline or when `LAPEL_COMMUNITY_ENABLED=false`).

### 4. `lapel community share`
Allows manual submission of session metrics (`--model`, `--provider`, `--vram`, `--batch-size`, `--token-rate`, `--dry-run`).

---

## REST API Contract

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/reports` | `POST` | Submit an anonymized `community.Report` payload |
| `/api/v1/reports/{hash}` | `GET` | Retrieve aggregated report count, average VRAM, and average `tok/s` for a hardware hash |
| `/api/v1/recommendations?hash={hash}&limit=5` | `GET` | Retrieve ranked `[]community.Recommendation` entries matched by exact hash or 8-char GPU/VRAM tier prefix |
