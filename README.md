# lapel

**Local Agent Protocol Enhancement Layer**

Personal CLI tool for bridging Windows local LLM servers (Ollama, LM Studio) with Linux coding tools inside WSL2.

## Why

WSL2 NAT mode changes the host gateway IP on every reboot, `/etc/resolv.conf` gets overwritten or points at the wrong DNS proxy, and default Microsoft Store Ubuntu distros eat gigs of RAM with systemd/GUI bloat while you're trying to keep weights in VRAM.

Work in progress.