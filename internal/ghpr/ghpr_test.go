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
	run := "https://github.com/abit2/chores/actions/runs/32031873044"
	if RepoFromRef(run) != "abit2/chores" {
		t.Fatalf("run repo=%q", RepoFromRef(run))
	}
	if id := RunIDFromRef(run); id != 32031873044 {
		t.Fatalf("run id=%d", id)
	}
	if !IsRunRef(run) {
		t.Fatal("expected run ref")
	}
}

func TestSameRun(t *testing.T) {
	a := Snapshot{Kind: KindRun, RunID: 32031873044, Repo: "abit2/chores", URL: "https://github.com/abit2/chores/actions/runs/32031873044"}
	b := Snapshot{Kind: KindRun, Input: "https://github.com/abit2/chores/actions/runs/32031873044", RunID: 32031873044}
	if !Same(a, b) {
		t.Fatal("expected same run")
	}
	pr := Snapshot{Kind: KindPR, Repo: "abit2/chores", Number: 1}
	if Same(a, pr) {
		t.Fatal("run should not match PR")
	}
}

func TestJobBucket(t *testing.T) {
	if jobBucket("in_progress", "") != "pending" {
		t.Fatal("in progress")
	}
	if jobBucket("completed", "success") != "pass" {
		t.Fatal("success")
	}
	if jobBucket("completed", "failure") != "fail" {
		t.Fatal("failure")
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

func TestFetchRun(t *testing.T) {
	if os.Getenv("LIVE_GH") == "" {
		t.Skip("set LIVE_GH=1 to hit GitHub")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	s := FetchRun(ctx, "https://github.com/abit2/chores/actions/runs/32031873044")
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	if s.Kind != KindRun {
		t.Fatalf("kind=%s", s.Kind)
	}
	if s.RunID != 32031873044 {
		t.Fatalf("run id=%d", s.RunID)
	}
	if s.WorkflowName == "" {
		t.Fatal("empty workflow")
	}
	if !Summarize(s.Checks).Finished() {
		t.Fatalf("expected finished, checks=%+v", s.Checks)
	}
}

func TestNeedsPRView(t *testing.T) {
	if needsPRView(Snapshot{Kind: KindJira, IssueKey: "TASK-1"}) {
		t.Fatal("jira")
	}
	if needsPRView(Snapshot{Kind: KindRun, RunID: 1}) {
		t.Fatal("run")
	}
	if !needsPRView(Snapshot{Input: "https://github.com/cli/cli/pull/1"}) {
		t.Fatal("new PR needs view")
	}
	if needsPRView(Snapshot{Kind: KindPR, Number: 1, URL: "https://github.com/cli/cli/pull/1"}) {
		t.Fatal("already viewed")
	}
}

func TestSkipGitHubPoll(t *testing.T) {
	pending := Snapshot{Kind: KindPR, Number: 1, URL: "https://github.com/cli/cli/pull/1", Checks: []Check{{Bucket: "pending"}}}
	if SkipGitHubPoll(pending) {
		t.Fatal("pending CI must still poll")
	}
	done := Snapshot{Kind: KindPR, Number: 1, URL: "https://github.com/cli/cli/pull/1", Checks: []Check{{Bucket: "pass"}}}
	if !SkipGitHubPoll(done) {
		t.Fatal("finished CI should skip interval polls")
	}
	jira := Snapshot{Kind: KindJira, IssueKey: "TASK-1"}
	if SkipGitHubPoll(jira) {
		t.Fatal("jira still comes from Slack")
	}
}
