package jira

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abit2/chores/internal/macnotify"
)

func TestParseBrowseURL(t *testing.T) {
	ref, ok := Parse("https://xyz-company.atlassian.net/browse/TASK-5947")
	if !ok {
		t.Fatal("expected parse")
	}
	if ref.Host != "xyz-company.atlassian.net" || ref.Key != "TASK-5947" {
		t.Fatalf("got %+v", ref)
	}
	if ref.BrowseURL() != "https://xyz-company.atlassian.net/browse/TASK-5947" {
		t.Fatalf("url=%s", ref.BrowseURL())
	}
}

func TestParseBrowseURLWithQuery(t *testing.T) {
	ref, ok := Parse("https://xyz-company.atlassian.net/browse/TASK-5947?focusedCommentId=1")
	if !ok || ref.Key != "TASK-5947" {
		t.Fatalf("got ok=%v %+v", ok, ref)
	}
}

func TestParseSelectedIssue(t *testing.T) {
	ref, ok := Parse("https://xyz-company.atlassian.net/jira/software/c/projects/TASK/boards/1?selectedIssue=TASK-5947")
	if !ok {
		t.Fatal("expected parse")
	}
	if ref.Host != "xyz-company.atlassian.net" || ref.Key != "TASK-5947" {
		t.Fatalf("got %+v", ref)
	}
}

func TestParseKeyUsesSite(t *testing.T) {
	t.Setenv("JIRA_SITE", "https://xyz-company.atlassian.net/")
	ref, ok := Parse("task-5947")
	if !ok {
		t.Fatal("expected parse")
	}
	if ref.Host != "xyz-company.atlassian.net" || ref.Key != "TASK-5947" {
		t.Fatalf("got %+v", ref)
	}
}

func TestParseRejectsGitHub(t *testing.T) {
	if _, ok := Parse("https://github.com/org/repo/pull/123"); ok {
		t.Fatal("github PR is not jira")
	}
	if IsRef("42") {
		t.Fatal("bare number is not jira")
	}
}

func TestApplyNotesMatchesKey(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	issue := Issue{Key: "TASK-5947", Host: "xyz-company.atlassian.net"}
	applyNotes(&issue, []macnotify.Note{
		{
			Title:     "Jira",
			Body:      "Ada commented on TASK-5947 Fix the picker",
			Delivered: time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC),
		},
	})
	if issue.Title != "Ada commented on TASK-5947 Fix the picker" {
		t.Fatalf("title=%q", issue.Title)
	}
	if issue.Status != "via Slack" {
		t.Fatalf("status=%q", issue.Status)
	}
	if issue.Updated == "" {
		t.Fatal("expected updated")
	}
}

func TestGroupNotesByKey(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	issues := groupNotes([]macnotify.Note{
		{Title: "Jira", Body: "assigned TASK-1", Delivered: time.Unix(1, 0)},
		{Title: "Jira", Body: "commented TASK-1 later", Delivered: time.Unix(2, 0)},
		{Title: "Jira", Body: "also TASK-2", Delivered: time.Unix(3, 0)},
	})
	if len(issues) != 2 {
		t.Fatalf("len=%d %+v", len(issues), issues)
	}
	if issues[0].Key != "TASK-1" || !strings.Contains(issues[0].Title, "later") {
		t.Fatalf("first=%+v", issues[0])
	}
	if issues[1].Key != "TASK-2" {
		t.Fatalf("second=%+v", issues[1])
	}
}

func TestFetchUnknownRef(t *testing.T) {
	issue := Fetch(context.Background(), "not-jira")
	if issue.Err == nil {
		t.Fatal("expected error")
	}
}
