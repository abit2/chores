package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch.json")
	t.Setenv("CHORES_WATCH_FILE", path)

	in := Watch{
		URLs:   []string{"https://github.com/cli/cli/pull/1", "https://github.com/cli/cli/pull/1"},
		Repo:   "abit2/chores",
		Hidden: []string{"https://github.com/abit2/chores/actions/runs/1"},
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.URLs) != 1 || out.URLs[0] != "https://github.com/cli/cli/pull/1" {
		t.Fatalf("urls=%v", out.URLs)
	}
	if out.Repo != "abit2/chores" {
		t.Fatalf("repo=%q", out.Repo)
	}
	if len(out.Hidden) != 1 {
		t.Fatalf("hidden=%v", out.Hidden)
	}
}

func TestLoadMissing(t *testing.T) {
	t.Setenv("CHORES_WATCH_FILE", filepath.Join(t.TempDir(), "missing.json"))
	w, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(w.URLs) != 0 || w.Repo != "" {
		t.Fatalf("got %+v", w)
	}
}

func TestMergeURLs(t *testing.T) {
	got := MergeURLs(
		[]string{"https://github.com/cli/cli/pull/1"},
		[]string{"https://github.com/cli/cli/pull/2", "https://github.com/cli/cli/pull/1"},
	)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
