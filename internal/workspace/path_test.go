package workspace

import (
	"testing"
)

func TestPathResolver_HostToGuest(t *testing.T) {
	r := NewPathResolver()

	tests := []struct {
		name     string
		hostPath string
		want     string
		wantErr  bool
	}{
		{
			name:     "standard Windows path",
			hostPath: `C:\Users\kurt\projects`,
			want:     "/mnt/c/Users/kurt/projects",
		},
		{
			name:     "root drive",
			hostPath: `D:\`,
			want:     "/mnt/d/",
		},
		{
			name:     "already Linux path",
			hostPath: "/home/agent/workspace",
			want:     "/home/agent/workspace",
		},
		{
			name:     "forward slashes",
			hostPath: `C:/Users/kurt/projects`,
			want:     "/mnt/c/Users/kurt/projects",
		},
		{
			name:     "nested path",
			hostPath: `E:\deep\nested\path\to\file.txt`,
			want:     "/mnt/e/deep/nested/path/to/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.HostToGuest(tt.hostPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("HostToGuest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("HostToGuest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathResolver_GuestToHost(t *testing.T) {
	r := NewPathResolver()

	tests := []struct {
		name     string
		guestPath string
		want     string
		wantErr  bool
	}{
		{
			name:      "standard mount path",
			guestPath: "/mnt/c/Users/kurt/projects",
			want:      `C:\Users\kurt\projects`,
		},
		{
			name:      "root drive",
			guestPath: "/mnt/d/",
			want:      `D:\`,
		},
		{
			name:      "non-mount path returns empty",
			guestPath: "/home/agent/workspace",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.GuestToHost(tt.guestPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("GuestToHost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GuestToHost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathResolver_RoundTrip(t *testing.T) {
	r := NewPathResolver()

	hostPath := `C:\Users\kurt\projects\repo`
	guest, err := r.HostToGuest(hostPath)
	if err != nil {
		t.Fatalf("HostToGuest failed: %v", err)
	}

	hostBack, err := r.GuestToHost(guest)
	if err != nil {
		t.Fatalf("GuestToHost failed: %v", err)
	}

	if hostBack != hostPath {
		t.Errorf("round-trip failed: %s → %s → %s", hostPath, guest, hostBack)
	}
}

func TestGuestHomePath(t *testing.T) {
	tests := []struct {
		user string
		name string
		want string
	}{
		{"agent", "workspace", "/home/agent/workspace"},
		{"kurt", "project", "/home/kurt/project"},
	}

	for _, tt := range tests {
		got := GuestHomePath(tt.user, tt.name)
		if got != tt.want {
			t.Errorf("GuestHomePath(%q, %q) = %v, want %v", tt.user, tt.name, got, tt.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/home/agent/workspace/", "/home/agent/workspace"},
		{"/", "/"},
		{"C:\\Users\\kurt", "C:/Users/kurt"},
	}

	for _, tt := range tests {
		got := Normalize(tt.input)
		if got != tt.want {
			t.Errorf("Normalize(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
