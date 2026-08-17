package ghpr

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseRefs(t *testing.T) {
	got := ParseRefs([]string{
		"https://github.com/cli/cli/pull/1, https://github.com/cli/cli/pull/2",
		"https://github.com/cli/cli/pull/1",
		"  42  ",
	})
	want := []string{
		"https://github.com/cli/cli/pull/1",
		"https://github.com/cli/cli/pull/2",
		"42",
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ref[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestRepoFromRef(t *testing.T) {
	got := RepoFromRef("https://github.com/cli/cli/pull/9079/files")
	if got != "cli/cli" {
		t.Fatalf("got %q", got)
	}
	if n := NumberFromRef("https://github.com/cli/cli/pull/9079/files"); n != 9079 {
		t.Fatalf("number=%d", n)
	}
}

func TestSamePR(t *testing.T) {
	a := Snapshot{Input: "https://github.com/cli/cli/pull/1", Repo: "cli/cli", Number: 1, URL: "https://github.com/cli/cli/pull/1"}
	b := Snapshot{Input: "https://github.com/cli/cli/pull/1/files", Repo: "cli/cli", Number: 1}
	if !SamePR(a, b) {
		t.Fatal("expected same PR for URL variants")
	}
	c := Snapshot{Input: "https://github.com/cli/cli/pull/2", Repo: "cli/cli", Number: 2}
	if SamePR(a, c) {
		t.Fatal("different numbers should not match")
	}
}

func TestSummarizeFinished(t *testing.T) {
	s := Summarize([]Check{
		{Bucket: "pass", State: "SUCCESS"},
		{Bucket: "pending", State: "IN_PROGRESS"},
		{Bucket: "skipping", State: "SKIPPED"},
	})
	if s.Pass != 1 || s.Pending != 1 || s.Skip != 1 {
		t.Fatalf("summary %+v", s)
	}
	if s.Finished() {
		t.Fatal("expected not finished while pending")
	}

	done := Summarize([]Check{
		{Bucket: "pass", State: "SUCCESS"},
		{Bucket: "fail", State: "FAILURE"},
	})
	if !done.Finished() || done.Passed() {
		t.Fatalf("expected finished failure, got %+v passed=%v", done, done.Passed())
	}
	if done.Outcome() != "failed" {
		t.Fatalf("outcome=%q", done.Outcome())
	}

	empty := Summarize(nil)
	if empty.Finished() {
		t.Fatal("empty checks are not finished")
	}
}

func TestFormatDuration(t *testing.T) {
	if got := FormatDuration(45 * time.Second); got != "45s" {
		t.Fatalf("got %q", got)
	}
	if got := FormatDuration(2*time.Minute + 3*time.Second); got != "2m 3s" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchPublicPR(t *testing.T) {
	if os.Getenv("LIVE_GH") == "" {
		t.Skip("set LIVE_GH=1 to hit GitHub")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	s := Fetch(ctx, "https://github.com/cli/cli/pull/14157", false)
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	if s.Number != 14157 {
		t.Fatalf("number=%d", s.Number)
	}
	if s.Repo != "cli/cli" {
		t.Fatalf("repo=%q", s.Repo)
	}
	if s.Title == "" {
		t.Fatal("empty title")
	}
}
