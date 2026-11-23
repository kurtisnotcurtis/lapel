package wsl

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"lapel/internal/domain"
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
	} else if !strings.Contains(err.Error(), "unsupported Ubuntu version") {
		t.Errorf("unexpected error for unknown Ubuntu version: %v", err)
	}
}

func TestRootfsURLDebian(t *testing.T) {
	for _, ver := range []string{"11", "12", "13"} {
		if got, err := RootfsURL("debian", ver); err == nil {
			t.Errorf("RootfsURL(debian,%s) = %q, want error", ver, got)
		} else if !strings.Contains(err.Error(), "ubuntu") {
			t.Errorf("RootfsURL(debian,%s) error should point to ubuntu, got: %v", ver, err)
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

func TestSanitizeDefaultUsername(t *testing.T) {
	cases := map[string]string{
		"Kurt":        "kurt",
		"John.Doe":    "john_doe",
		"Alice Smith": "alice_smith",
		"123user":     "u123user",
		"   ":         "",
	}
	for in, want := range cases {
		got := sanitizeDefaultUsername(in)
		if got != want {
			t.Errorf("sanitizeDefaultUsername(%q) = %q, want %q", in, got, want)
		}
		if got != "" {
			if err := validateUsername(got); err != nil {
				t.Errorf("sanitized username %q failed validation: %v", got, err)
			}
		}
	}
}

func TestDefaultProvisionOptionsCarriesBaseAndVersion(t *testing.T) {
	opts := DefaultProvisionOptions(domain.Environment{
		Distribution: "custom-distro",
		Base:         "ubuntu",
		Version:      "22.04",
	})
	if opts.Name != "custom-distro" {
		t.Errorf("Name = %q, want custom-distro", opts.Name)
	}
	if opts.Base != "ubuntu" || opts.Version != "22.04" {
		t.Errorf("Base/Version = %q/%q, want ubuntu/22.04", opts.Base, opts.Version)
	}
}

func TestImportMinimalDistroRejectsInvalidUserBeforeImport(t *testing.T) {
	err := ImportMinimalDistro(t.Context(), ProvisionOptions{
		Name:        "should-never-import",
		Base:        "ubuntu",
		Version:     "24.04",
		DefaultUser: "John.Doe",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid default user") {
		t.Fatalf("expected upfront invalid default user error, got %v", err)
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

func TestDistroStorageDirUsesLocalAppData(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LOCALAPPDATA", tmp)

	dir, err := DistroStorageDir("lapel")
	if err != nil {
		t.Fatalf("DistroStorageDir: %v", err)
	}
	if !strings.HasPrefix(dir, tmp) || !strings.HasSuffix(dir, "lapel") {
		t.Errorf("unexpected storage dir %q (want under %q ending in lapel)", dir, tmp)
	}
}

func TestDefaultProvisionOptionsMergesExtraPackages(t *testing.T) {
	opts := DefaultProvisionOptions(domain.Environment{
		Name:     "lapel",
		Packages: []string{"htop", "git", "sqlite3"}, // "git" is already in AgentToolkitPackages
	})
	foundHtop, foundSqlite, gitCount := false, false, 0
	for _, p := range opts.Packages {
		switch p {
		case "htop":
			foundHtop = true
		case "sqlite3":
			foundSqlite = true
		case "git":
			gitCount++
		}
	}
	if !foundHtop || !foundSqlite {
		t.Errorf("expected extra packages htop and sqlite3 in %v", opts.Packages)
	}
	if gitCount != 1 {
		t.Errorf("expected git to appear once (deduplicated), got %d", gitCount)
	}
}

func TestAgentToolkitPackagesUsesValidUbuntuPackages(t *testing.T) {
	hasYQ := false
	for _, pkg := range AgentToolkitPackages {
		if pkg == "libyaml-tools" {
			t.Errorf("AgentToolkitPackages contains non-existent Ubuntu package %q", pkg)
		}
		if pkg == "yq" {
			hasYQ = true
		}
	}
	if !hasYQ {
		t.Errorf("expected AgentToolkitPackages to include yq")
	}
}

func TestEnsureUbuntuUniverseScriptTargetsDEB822AndLegacy(t *testing.T) {
	script := ensureUbuntuUniverseScript()
	for _, want := range []string{
		"/etc/apt/sources.list.d/ubuntu.sources",
		"/etc/apt/sources.list",
		"universe",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("ensureUbuntuUniverseScript missing %q", want)
		}
	}
}

func TestLinePrefixWriterStripsNULsAndPrefixesLines(t *testing.T) {
	var out bytes.Buffer
	pw := newLinePrefixWriter(&out, "    ")
	_, _ = pw.Write([]byte("R\x00eading package lists...\r\n\nBuilding tree\x00"))
	pw.Flush()

	want := "    Reading package lists...\n    Building tree\n"
	if out.String() != want {
		t.Errorf("linePrefixWriter output = %q, want %q", out.String(), want)
	}
}

func TestEnsureDNSResolvScriptHandlesSymlinkAndStub(t *testing.T) {
	script := ensureDNSResolvScript()
	for _, want := range []string{
		"-L /etc/resolv.conf",
		`127\.0\.0\.53`,
		"rm -f /etc/resolv.conf",
		"nameserver 8.8.8.8",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("ensureDNSResolvScript missing %q", want)
		}
	}
}

func TestUvInstallScriptUsesUnmanagedInstallEnv(t *testing.T) {
	script := uvInstallScript()
	if strings.Contains(script, "--install-dir") {
		t.Errorf("uvInstallScript should not pass --install-dir to uv install.sh")
	}
	for _, want := range []string{
		"UV_UNMANAGED_INSTALL=/usr/local/uv/bin",
		"UV_PYTHON_INSTALL_DIR=/usr/local/uv/python",
		`PATH="/usr/local/uv/bin:$PATH"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("uvInstallScript missing %q", want)
		}
	}
}

func TestDownloadProgressReaderReportsMilestones(t *testing.T) {
	const total = 100
	data := bytes.Repeat([]byte("a"), total)
	var out bytes.Buffer
	pr := newDownloadProgressReader(bytes.NewReader(data), total, &out)
	buf := make([]byte, 25)
	for {
		_, err := pr.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
	}
	pr.Finish()

	got := out.String()
	for _, want := range []string{"downloaded 20%", "downloaded 40%", "downloaded 60%", "downloaded 80%", "download complete"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in progress output:\n%s", want, got)
		}
	}
}

func TestFreshDistroSetupScriptUsesValidInteropKey(t *testing.T) {
	modern := freshDistroSetupScript("kurt", 26100, true)
	if !strings.Contains(modern, "[interop]\nappendWindowsPath=false") {
		t.Errorf("expected singular [interop] appendWindowsPath=false for modern WSL build:\n%s", modern)
	}
	if strings.Contains(modern, "appendWindowsPaths") {
		t.Errorf("must not emit invalid plural key appendWindowsPaths:\n%s", modern)
	}
	if !strings.Contains(modern, "[boot]\nsystemd=true") {
		t.Errorf("expected [boot] systemd=true when enabled:\n%s", modern)
	}

	noSystemd := freshDistroSetupScript("kurt", 26100, false)
	if !strings.Contains(noSystemd, "[boot]\nsystemd=false\ncommand="+BootHookPath) {
		t.Errorf("expected [boot] systemd=false with boot hook when disabled:\n%s", noSystemd)
	}
	if !strings.Contains(noSystemd, "cat > "+BootHookPath) {
		t.Errorf("expected boot hook installation when systemd=false:\n%s", noSystemd)
	}

	fallback := freshDistroSetupScript("kurt", 0, true)
	if !strings.Contains(fallback, "[interop]\nappendWindowsPath=false") {
		t.Errorf("expected [interop] appendWindowsPath=false when build detection returns 0:\n%s", fallback)
	}

	legacy := freshDistroSetupScript("kurt", 17000, true)
	if strings.Contains(legacy, "[interop]\n") {
		t.Errorf("did not expect [interop] section in wsl.conf for pre-17093 build:\n%s", legacy)
	}
}
