package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abit2/chores/internal/ghpr"
)

func TestSaveLoadPRs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHORES_WATCH_FILE", filepath.Join(dir, "watch.json"))
	t.Setenv("CHORES_PR_FILE", "")

	at := time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)
	in := ghpr.Snapshot{
		Kind:               ghpr.KindPR,
		Input:              "https://github.com/org/repo/pull/1032",
		Number:             1032,
		Title:              "fix the widget",
		URL:                "https://github.com/org/repo/pull/1032",
		State:              "OPEN",
		Repo:               "org/repo",
		Author:             "abit2",
		HeadRefName:        "fix/widget",
		UnresolvedComments: 2,
		Checks:             []ghpr.Check{{Name: "test", Bucket: "pass"}},
		FetchedAt:          at,
	}
	if err := SavePRs([]ghpr.Snapshot{in}); err != nil {
		t.Fatal(err)
	}
	path, err := PRsPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "prs.json") {
		t.Fatalf("path=%s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]PRRecord
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	rec, ok := raw["org/repo#1032"]
	if !ok {
		t.Fatalf("keys=%v", raw)
	}
	if rec.Title != "fix the widget" || rec.RefreshedAt.UTC() != at || rec.UnresolvedComments != 2 {
		t.Fatalf("record=%+v", rec)
	}

	cache, err := LoadPRs()
	if err != nil {
		t.Fatal(err)
	}
	seed := ghpr.Snapshot{Kind: ghpr.KindPR, Input: "https://github.com/org/repo/pull/1032", Repo: "org/repo"}
	got, ok := LookupPR(cache, seed)
	if !ok {
		t.Fatal("lookup missed")
	}
	snap := got.Snapshot()
	if snap.Number != 1032 || snap.Title != "fix the widget" || len(snap.Checks) != 1 || snap.UnresolvedComments != 2 {
		t.Fatalf("snap=%+v", snap)
	}
	if !snap.FetchedAt.Equal(at) {
		t.Fatalf("fetchedAt=%s", snap.FetchedAt)
	}
}

func TestSavePRsSkipsJira(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHORES_PR_FILE", filepath.Join(dir, "prs.json"))
	if err := SavePRs([]ghpr.Snapshot{{
		Kind:     ghpr.KindJira,
		IssueKey: "TASK-1",
		Title:    "nope",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prs.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no file, err=%v", err)
	}
}

func TestSavePRsMerges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prs.json")
	t.Setenv("CHORES_PR_FILE", path)
	first := ghpr.Snapshot{Kind: ghpr.KindPR, Repo: "org/repo", Number: 1, Title: "one", FetchedAt: time.Unix(1, 0).UTC()}
	second := ghpr.Snapshot{Kind: ghpr.KindPR, Repo: "org/repo", Number: 2, Title: "two", FetchedAt: time.Unix(2, 0).UTC()}
	if err := SavePRs([]ghpr.Snapshot{first}); err != nil {
		t.Fatal(err)
	}
	if err := SavePRs([]ghpr.Snapshot{second}); err != nil {
		t.Fatal(err)
	}
	cache, err := LoadPRs()
	if err != nil {
		t.Fatal(err)
	}
	if cache["org/repo#1"].Title != "one" || cache["org/repo#2"].Title != "two" {
		t.Fatalf("%+v", cache)
	}
}
