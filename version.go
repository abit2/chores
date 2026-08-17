package main

import (
	"runtime/debug"
	"strings"
)

// version is overwritten by -ldflags at release time, e.g. make dist.
var version = "dev"

func resolveVersion() string {
	return versionFrom(version, debug.ReadBuildInfo)
}

func versionFrom(ldflag string, read func() (*debug.BuildInfo, bool)) string {
	if v := strings.TrimSpace(ldflag); v != "" && v != "dev" {
		return v
	}
	info, ok := read()
	if !ok || info == nil {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if modified {
		return rev + "-dirty"
	}
	return rev
}
