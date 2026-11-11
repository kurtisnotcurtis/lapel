package wsl

import (
	"strings"

	"testing"
)

func TestRootfsURLUbuntu(t *testing.T) {
	got, err := RootfsURL("ubuntu", "24.04")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64-root.tar.xz"
	if got != want {
		t.Errorf("RootfsURL(ubuntu,24.04) = %q, want %q", got, want)
	}
}

func TestRootfsURLDefaultsToLatestUbuntu(t *testing.T) {
	got, err := RootfsURL("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64-root.tar.xz"
	if got != want {
		t.Errorf("RootfsURL() default = %q, want %q", got, want)
	}
}

func TestRootfsURLUnknownUbuntuVersion(t *testing.T) {
	if _, err := RootfsURL("ubuntu", "99.99"); err == nil {
		t.Error("expected error for unknown Ubuntu version 99.99")
	} else if !strings.Contains(err.Error(), "RootFSImageOverride") {
		t.Errorf("error should mention RootFSImageOverride, got: %v", err)
	}
}

func TestRootfsURLDebian(t *testing.T) {
	// Debian's cloud mirrors only publish full disk images (checked
	// bookworm and trixie in 2026-07), which wsl --import cannot consume, so
	// RootfsURL must fail with a pointer to the override knob.
	for _, ver := range []string{"11", "12", "13"} {
		if got, err := RootfsURL("debian", ver); err == nil {
			t.Errorf("RootfsURL(debian,%s) = %q, want error", ver, got)
		} else if !strings.Contains(err.Error(), "RootFSImageOverride") {
			t.Errorf("RootfsURL(debian,%s) error should mention RootFSImageOverride, got: %v", ver, err)
		}
	}
}

func TestRootfsURLUnsupported(t *testing.T) {
	if _, err := RootfsURL("fedora", "39"); err == nil {
		t.Errorf("expected error for unsupported base fedora")
	}
}

func TestDefaultVersion(t *testing.T) {
	if got := DefaultVersion(""); got != "24.04" {
		t.Errorf("default ubuntu version = %q, want 24.04", got)
	}
	if got := DefaultVersion("debian"); got != "12" {
		t.Errorf("default debian version = %q, want 12", got)
	}
}

func TestCodenameForDebian(t *testing.T) {
	cases := map[string]string{
		"11": "bullseye",
		"12": "bookworm",
		"99": "bookworm", // unknown falls back to current stable codename
	}
	for ver, want := range cases {
		if got := codenameForDebian(ver); got != want {
			t.Errorf("codenameForDebian(%s) = %q, want %q", ver, got, want)
		}
	}
}

func TestParseInstalledPackages(t *testing.T) {
	// Exact output shape of `dpkg-query -W -f=${Package}\t${db:Status-Status}\n`.
	input := "git\tinstalled\ncurl\tremoved\nwget\thalf-configured\n"
	got := parseInstalledPackages(input)
	if !got["git"] {
		t.Errorf("git should be installed")
	}
	if got["curl"] || got["wget"] {
		t.Errorf("non-installed states must not count: %+v", got)
	}

	// Regression: the old ${Status} field emitted "install ok installed";
	// that shape must NOT be treated as installed (it never matched before,
	// but the new parser must also not false-positive on it).
	old := parseInstalledPackages("git\tinstall ok installed")
	if old["git"] {
		t.Errorf("three-word legacy status line should not count as installed")
	}
}
