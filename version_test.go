package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromLdflag(t *testing.T) {
	got := versionFrom("v1.2.3", func() (*debug.BuildInfo, bool) {
		t.Fatal("should not read build info when ldflag is set")
		return nil, false
	})
	if got != "v1.2.3" {
		t.Fatalf("got %q", got)
	}
}

func TestVersionFromGoInstall(t *testing.T) {
	got := versionFrom("dev", func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.1.1"}}, true
	})
	if got != "v0.1.1" {
		t.Fatalf("got %q", got)
	}
}

func TestVersionFromDevelUsesVCS(t *testing.T) {
	got := versionFrom("dev", func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef123456"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	})
	if got != "abcdef1-dirty" {
		t.Fatalf("got %q", got)
	}
}

func TestVersionFallbackDev(t *testing.T) {
	got := versionFrom("dev", func() (*debug.BuildInfo, bool) {
		return nil, false
	})
	if got != "dev" {
		t.Fatalf("got %q", got)
	}
}
