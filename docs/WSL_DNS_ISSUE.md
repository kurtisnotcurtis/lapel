# WSL DNS Resolution Issue

## Problem

Go's HTTP client uses Windows' DNS resolver (`getaddrinfow`), which can fail to resolve certain domains like `cloud-images.ubuntu.com`, even when bash/nslookup inside WSL works fine using WSL's internal DNS resolver (NAT gateway `10.255.255.254`).

### Symptoms

```
Get "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64-root.tar.xz": 
dial tcp: lookup cloud-images.ubuntu.com: getaddrinfow: This is usually a temporary error during hostname resolution and means that the local server did not receive a response from an authoritative server.
```

### Why It Happens

| Resolver | Source | Behavior |
|----------|--------|----------|
| **Windows DNS** (Go uses this) | `getaddrinfow` API | Can fail for certain domains, especially during high load or ISP DNS issues |
| **WSL Internal DNS** | `/etc/resolv.conf` → NAT gateway `10.255.255.254` | Usually works but can be flaky for some domains |

## Mitigation Strategies

### 1. Retry with Exponential Backoff (Implemented)

The `downloadRootfs()` function now retries up to 3 times with exponential backoff:
- Attempt 1: Immediate
- Attempt 2: After 1s delay
- Attempt 3: After 2s delay

This handles transient DNS failures during e2e tests.

### 2. Force Google Public DNS in WSL (Implemented)

The provisioning flow now writes `/etc/wsl.conf` with:

```ini
[network]
generateResolvConf = false
```

And manually sets `/etc/resolv.conf`:

```conf
nameserver 8.8.8.8
nameserver 8.8.4.4
```

This ensures WSL uses Google's reliable DNS servers for internal operations (apt, curl, etc.).

### 3. IP Address Fallback (Workaround)

When DNS consistently fails, use the known IP address directly:

```bash
# cloud-images.ubuntu.com resolves to:
185.125.190.37
185.125.190.40
```

Example in e2e test URL override:
```go
url = "https://185.125.190.37/noble/current/noble-server-cloudimg-amd64-root.tar.xz"
```

### 4. Force Go to Use WSL DNS (Experimental)

Set environment variable before running tests:

```bash
GODEBUG=netdns=cgo go test ./internal/backend/wsl/
```

This forces Go to use the C library's `getaddrinfo()` instead of Windows' native API, which may resolve differently.

## Verification

### Check WSL DNS Configuration

```bash
wsl -d Ubuntu-Lean -- cat /etc/resolv.conf
# Should show: nameserver 8.8.8.8 (or your chosen DNS)

wsl -d Ubuntu-Lean -- cat /etc/wsl.conf | grep -A 3 network
# Should show: generateResolvConf = false
```

### Test DNS Resolution from WSL

```bash
wsl -d Ubuntu-Lean -- nslookup cloud-images.ubuntu.com
# Should resolve to 185.125.190.37 or similar
```

### Test DNS Resolution from Go (Windows)

Run e2e test and observe retry messages:
```
[retry] rootfs download attempt 2/3, waiting 1s
[retry] rootfs download attempt 3/3, waiting 2s
```

If all retries fail consistently, use IP address fallback.

## Related Files

- `internal/backend/wsl/provision.go` - `downloadRootfs()` with retry logic
- `internal/backend/wsl/provision.go` - `setupFreshDistro()` writes `/etc/wsl.conf` and `/etc/resolv.conf`
- `docs/E2E_TESTING.md` - E2E test documentation

## References

- [WSL DNS Resolution Issues](https://github.com/microsoft/WSL/issues/4580)
- [Go net package DNS resolution](https://pkg.go.dev/net#hdr-Name_Resolution)
- [Ubuntu Cloud Images](https://cloud-images.ubuntu.com/)
