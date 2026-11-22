package wsl

import (
	"strings"
	"testing"

	"lapel/internal/domain"
)

func TestResolveSystemd(t *testing.T) {
	if !ResolveSystemd(domain.Environment{}) {
		t.Error("expected nil Systemd to default to true")
	}
	tr, fa := true, false
	if !ResolveSystemd(domain.Environment{Systemd: &tr}) {
		t.Error("expected &true to resolve to true")
	}
	if ResolveSystemd(domain.Environment{Systemd: &fa}) {
		t.Error("expected &false to resolve to false")
	}
}

func TestUpdateWSLConfBoot(t *testing.T) {
	existing := `[boot]
systemd=true

[automount]
enabled = true

[network]
generateResolvConf = false

[user]
default = kurt

[interop]
appendWindowsPath=false
`
	disabled := updateWSLConfBoot(existing, false)
	wantBootOff := "[boot]\nsystemd=false\ncommand=" + BootHookPath
	if !strings.Contains(disabled, wantBootOff) {
		t.Errorf("disabled wsl.conf missing %q:\n%s", wantBootOff, disabled)
	}
	for _, keep := range []string{"[automount]\nenabled = true", "[network]\ngenerateResolvConf = false", "[user]\ndefault = kurt", "[interop]\nappendWindowsPath=false"} {
		if !strings.Contains(disabled, keep) {
			t.Errorf("disabled wsl.conf dropped section %q:\n%s", keep, disabled)
		}
	}

	reenabled := updateWSLConfBoot(disabled, true)
	if !strings.Contains(reenabled, "[boot]\nsystemd=true\n") {
		t.Errorf("re-enabled wsl.conf missing [boot] systemd=true:\n%s", reenabled)
	}
	if strings.Contains(reenabled, BootHookPath) {
		t.Errorf("re-enabled wsl.conf should remove boot hook command:\n%s", reenabled)
	}
	if !strings.Contains(reenabled, "[user]\ndefault = kurt") {
		t.Errorf("re-enabled wsl.conf dropped [user] section:\n%s", reenabled)
	}

	// File without a [boot] section prepends one cleanly.
	noBoot := "[user]\ndefault = kurt\n"
	prepended := updateWSLConfBoot(noBoot, false)
	if !strings.HasPrefix(prepended, wantBootOff+"\n\n[user]\ndefault = kurt\n") {
		t.Errorf("unexpected prepended wsl.conf:\n%q", prepended)
	}

	// Empty file emits just the [boot] section.
	if got := updateWSLConfBoot("", false); got != wantBootOff+"\n" {
		t.Errorf("empty wsl.conf -> %q, want %q", got, wantBootOff+"\n")
	}
}

func TestParseWSLConfBoot(t *testing.T) {
	cfg, cmd := parseWSLConfBoot("[boot]\nsystemd=false\ncommand=/usr/local/sbin/lapel-boot.sh\n\n[user]\ndefault = kurt\n")
	if cfg == nil || *cfg {
		t.Errorf("configured = %v, want false", cfg)
	}
	if cmd != BootHookPath {
		t.Errorf("command = %q, want %q", cmd, BootHookPath)
	}

	cfg, cmd = parseWSLConfBoot("# comment\n[boot]\n  systemd = true\n")
	if cfg == nil || !*cfg {
		t.Errorf("configured = %v, want true", cfg)
	}
	if cmd != "" {
		t.Errorf("command = %q, want empty", cmd)
	}

	cfg, cmd = parseWSLConfBoot("[user]\ndefault = kurt\n")
	if cfg != nil || cmd != "" {
		t.Errorf("expected nil/empty for conf without [boot], got (%v, %q)", cfg, cmd)
	}
}

func TestBuildBootHookAndSSHStartScripts(t *testing.T) {
	boot := BuildBootHookScript()
	for _, want := range []string{"#!/bin/sh", "mkdir -p -m 0755 /run/sshd", "ssh-keygen -A", "/usr/sbin/sshd"} {
		if !strings.Contains(boot, want) {
			t.Errorf("BuildBootHookScript missing %q:\n%s", want, boot)
		}
	}

	sshScript := buildSSHStartScript()
	for _, want := range []string{"mkdir -p -m 0755 /run/sshd", "/run/systemd/system", "systemctl restart ssh", "/usr/sbin/sshd"} {
		if !strings.Contains(sshScript, want) {
			t.Errorf("buildSSHStartScript missing %q:\n%s", want, sshScript)
		}
	}
}
