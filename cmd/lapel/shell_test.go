package main

import (
	"reflect"
	"testing"
)

func TestBuildShellWSLArgs(t *testing.T) {
	tests := []struct {
		name      string
		distro    string
		user      string
		cdDir     string
		extraArgs []string
		want      []string
	}{
		{
			name:   "defaults",
			distro: "lapel",
			cdDir:  "~",
			want:   []string{"-d", "lapel", "--cd", "~"},
		},
		{
			name:      "with user and command",
			distro:    "lapel",
			user:      "agent",
			cdDir:     ".",
			extraArgs: []string{"ls", "-la"},
			want:      []string{"-d", "lapel", "-u", "agent", "--cd", ".", "--", "ls", "-la"},
		},
		{
			name:   "empty cd omitted",
			distro: "custom",
			cdDir:  "",
			want:   []string{"-d", "custom"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildShellWSLArgs(tc.distro, tc.user, tc.cdDir, tc.extraArgs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildShellWSLArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildShellWTArgs(t *testing.T) {
	wslArgs := []string{"-d", "lapel", "--cd", "~"}
	got := buildShellWTArgs("lapel", "#0EA5E9", wslArgs)
	want := []string{"-w", "0", "new-tab", "--title", "lapel", "--tabColor", "#0EA5E9", "--", "wsl.exe", "-d", "lapel", "--cd", "~"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildShellWTArgs() = %v, want %v", got, want)
	}
}
