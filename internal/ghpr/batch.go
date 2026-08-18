package ghpr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FillPRViews loads PR metadata for snapshots that still need a view.
// GitHub's GraphQL API is point-limited and shared with `gh pr checks`, so
// we do not batch via graphql (see https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api).
func FillPRViews(ctx context.Context, snaps []Snapshot) {
	for i, s := range snaps {
		if !needsPRView(s) {
			continue
		}
		snaps[i] = loadPRView(ctx, s)
	}
}

func needsPRView(s Snapshot) bool {
	if s.Kind == KindRun || s.Kind == KindJira {
		return false
	}
	if s.RunID > 0 || IsRunRef(s.Input) {
		return false
	}
	if strings.HasPrefix(s.Input, "slack:") {
		return false
	}
	return s.Number == 0 || s.URL == ""
}

func loadPRView(ctx context.Context, snap Snapshot) Snapshot {
	snap.Kind = KindPR
	snap.FetchedAt = time.Now()
	ref := snap.Input
	if snap.URL != "" {
		ref = snap.URL
	}
	viewOut, err := runGH(ctx, "pr", "view", ref, "--json",
		"number,title,url,state,isDraft,headRefName,author")
	if err != nil {
		snap.Err = err
		return snap
	}
	var view prView
	if err := json.Unmarshal(viewOut, &view); err != nil {
		snap.Err = fmt.Errorf("decode pr view: %w", err)
		return snap
	}
	return applyPRView(snap, view)
}

func applyPRView(snap Snapshot, view prView) Snapshot {
	snap.Kind = KindPR
	snap.Err = nil
	snap.Number = view.Number
	snap.Title = view.Title
	snap.URL = view.URL
	snap.State = view.State
	snap.IsDraft = view.IsDraft
	snap.HeadRefName = view.HeadRefName
	snap.Author = view.Author.Login
	if snap.Repo == "" {
		snap.Repo = RepoFromRef(view.URL)
	}
	if snap.FetchedAt.IsZero() {
		snap.FetchedAt = time.Now()
	}
	return snap
}

// SkipGitHubPoll reports whether a later interval poll can skip this GitHub item.
// Finished CI is left alone until a manual refresh so we do not keep spending
// GraphQL points (gh pr checks / gh run view).
func SkipGitHubPoll(s Snapshot) bool {
	if s.Kind == KindJira {
		return false
	}
	if s.Err != nil {
		return false
	}
	if s.Kind == KindRun || s.RunID > 0 || IsRunRef(s.Input) {
		return Summarize(s.Checks).Finished()
	}
	if s.Number == 0 || s.URL == "" {
		return false
	}
	return Summarize(s.Checks).Finished()
}
