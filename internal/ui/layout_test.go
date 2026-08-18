package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/abit2/chores/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSmallWindowShowsSelectedCard(t *testing.T) {
	m := smallWatchModel()
	got, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	m = got.(model)

	m.selected = len(m.rows) - 1
	m.showBody()

	view := m.viewport.View()
	if !strings.Contains(view, "▶") {
		t.Fatalf("selected marker missing in viewport:\n%s", view)
	}
	key := m.rows[m.selected].snap.IssueKey
	if !strings.Contains(view, key) {
		t.Fatalf("selected %s not scrolled into view:\n%s", key, view)
	}
}

func TestSmallWindowCardsFitViewport(t *testing.T) {
	m := smallWatchModel()
	got, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	m = got.(model)

	for i, line := range strings.Split(m.body(), "\n") {
		if w := lipgloss.Width(line); w > m.viewport.Width {
			t.Errorf("line %d width %d > viewport %d", i, w, m.viewport.Width)
		}
	}

	full := m.View()
	if h := lipgloss.Height(full); h > 22 {
		t.Fatalf("view height %d > window 22\n%s", h, full)
	}
}

func TestSmallWindowFirstCardSelected(t *testing.T) {
	m := smallWatchModel()
	got, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	m = got.(model)

	view := m.viewport.View()
	if !strings.Contains(view, "▶") {
		t.Fatalf("selected marker missing on first card:\n%s", view)
	}
	if !strings.Contains(view, "#1032") {
		t.Fatalf("first PR missing:\n%s", view)
	}
}

func TestSmallWindowSelectionStayVisible(t *testing.T) {
	m := smallWatchModel()
	got, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	m = got.(model)

	for i := 0; i < len(m.rows); i++ {
		m.selected = i
		m.showBody()
		view := m.viewport.View()
		if !strings.Contains(view, "▶") {
			t.Fatalf("item %d: selected marker missing:\n%s", i, view)
		}
		want := m.rows[i].snap.IssueKey
		if want == "" {
			want = fmt.Sprintf("#%d", m.rows[i].snap.Number)
		}
		if !strings.Contains(view, want) {
			t.Fatalf("item %d: %s not visible:\n%s", i, want, view)
		}
	}
}

func smallWatchModel() model {
	m := New(Config{Interval: time.Minute}).(model)
	m.mode = modeWatch
	fail := make([]ghpr.Check, 0, 39)
	for i := 0; i < 20; i++ {
		fail = append(fail, ghpr.Check{Name: fmt.Sprintf("pass-%d", i), Bucket: "pass"})
	}
	for i := 0; i < 19; i++ {
		fail = append(fail, ghpr.Check{Name: fmt.Sprintf("fail-%d", i), Bucket: "fail"})
	}
	pass := make([]ghpr.Check, 0, 41)
	for i := 0; i < 41; i++ {
		pass = append(pass, ghpr.Check{Name: fmt.Sprintf("ok-%d", i), Bucket: "pass"})
	}
	m.rows = []prRow{
		{snap: ghpr.Snapshot{Kind: ghpr.KindPR, Number: 1032, Title: "fix the widget", Repo: "acme/repo", Author: "Arthur", HeadRefName: "fix/widget", Checks: fail}, notified: true},
		{snap: ghpr.Snapshot{Kind: ghpr.KindPR, Number: 996, Title: "ship it", Repo: "acme/repo", Author: "abit2", HeadRefName: "ship", Checks: pass}, notified: true},
		{snap: ghpr.Snapshot{Kind: ghpr.KindJira, IssueKey: "PROJ-3147", Repo: "acme.atlassian.net", Title: ""}},
		{snap: ghpr.Snapshot{Kind: ghpr.KindJira, IssueKey: "PROJ-3257", Repo: "acme.atlassian.net", Title: ""}},
		{snap: ghpr.Snapshot{Kind: ghpr.KindJira, IssueKey: "PROJ-3301", Repo: "acme.atlassian.net", Title: ""}},
		{snap: ghpr.Snapshot{Kind: ghpr.KindJira, IssueKey: "PROJ-3302", Repo: "acme.atlassian.net", Title: ""}},
		{snap: ghpr.Snapshot{Kind: ghpr.KindJira, IssueKey: "PROJ-3303", Repo: "acme.atlassian.net", Title: ""}},
	}
	return m
}

func TestKeepOnErrorPreservesChecks(t *testing.T) {
	prev := ghpr.Snapshot{
		Kind:   ghpr.KindPR,
		Number: 7,
		Title:  "old",
		Checks: []ghpr.Check{{Name: "ci", Bucket: "pass"}},
		Repo:   "org/repo",
		URL:    "https://github.com/org/repo/pull/7",
	}
	next := ghpr.Snapshot{Kind: ghpr.KindPR, Input: prev.URL, Err: fmt.Errorf("boom")}
	got := keepOnError(next, prev)
	if got.Title != "old" || got.Number != 7 || len(got.Checks) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Err == nil {
		t.Fatal("expected error")
	}
}

func TestWrapHelpKeepsShortcutLabels(t *testing.T) {
	got := wrapHelp(helpItems(), 60)
	for _, want := range []string{"enter expand", "o open", "r refresh", "R refresh all", "c clear notes", "N clear all notes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if lipgloss.Width(line) > 60 {
			t.Fatalf("line wider than 60: %q (%d)", line, lipgloss.Width(line))
		}
		if strings.HasSuffix(strings.TrimSpace(line), "·") {
			t.Fatalf("line ends mid-item: %q", line)
		}
	}
}

func TestRelativeFetchedUsesGivenTime(t *testing.T) {
	got := relativeFetched(time.Now().Add(-2 * time.Hour))
	if !strings.Contains(got, "2h") {
		t.Fatalf("got %q", got)
	}
}

func TestSkipGitHubPollCachedKeepsFetchedAt(t *testing.T) {
	snap := ghpr.Snapshot{Kind: ghpr.KindPR, Number: 1, FetchedAt: time.Now().Add(-time.Hour)}
	if !skipGitHubPoll(pollCached, snap) {
		t.Fatal("startup poll should skip cached GitHub items")
	}
	if skipGitHubPoll(pollAll, snap) {
		t.Fatal("force poll should not skip")
	}
}

func TestHydrateUsesSavedRefreshTime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHORES_WATCH_FILE", filepath.Join(dir, "watch.json"))
	t.Setenv("CHORES_PR_FILE", filepath.Join(dir, "prs.json"))
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	url := "https://github.com/org/repo/pull/1032"
	if err := store.Save(store.Watch{URLs: []string{url}, LastPoll: at}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePRs([]ghpr.Snapshot{{
		Kind:      ghpr.KindPR,
		Input:     url,
		Number:    1032,
		Repo:      "org/repo",
		URL:       url,
		Title:     "cached",
		FetchedAt: at,
	}}); err != nil {
		t.Fatal(err)
	}
	m := New(Config{Refs: []string{url}, Interval: time.Minute}).(model)
	if len(m.rows) != 1 {
		t.Fatalf("rows=%d", len(m.rows))
	}
	if !m.rows[0].snap.FetchedAt.Equal(at) {
		t.Fatalf("fetchedAt=%s want %s", m.rows[0].snap.FetchedAt, at)
	}
	if !m.lastPoll.Equal(at) {
		t.Fatalf("lastPoll=%s want %s", m.lastPoll, at)
	}
	if !m.hasGitHubCache() {
		t.Fatal("expected github cache")
	}
	if got := relativeFetched(m.rows[0].snap.FetchedAt); !strings.Contains(got, "2h") {
		t.Fatalf("relative=%q", got)
	}
}
