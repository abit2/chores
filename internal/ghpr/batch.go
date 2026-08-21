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

const prViewQuery = `query($owner:String!,$name:String!,$number:Int!){
  repository(owner:$owner,name:$name){
    pullRequest(number:$number){
      number
      title
      url
      state
      isDraft
      headRefName
      author { login }
      reviewThreads(first:100){ nodes { isResolved } }
    }
  }
}`

func loadPRView(ctx context.Context, snap Snapshot) Snapshot {
	snap.Kind = KindPR
	snap.FetchedAt = time.Now()
	repo, number := Identity(snap)
	if repo == "" {
		repo = RepoFromRef(snap.Input)
	}
	if number == 0 {
		number = NumberFromRef(snap.Input)
	}
	if repo != "" && number > 0 {
		return loadPRViewGraphQL(ctx, snap, repo, number)
	}
	return loadPRViewCLI(ctx, snap)
}

func loadPRViewCLI(ctx context.Context, snap Snapshot) Snapshot {
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
	snap = applyPRView(snap, view)
	repo, number := Identity(snap)
	if repo == "" || number == 0 {
		return snap
	}
	next := loadPRViewGraphQL(ctx, snap, repo, number)
	if next.Err != nil {
		snap.Err = nil
		return snap
	}
	return next
}

func loadPRViewGraphQL(ctx context.Context, snap Snapshot, repo string, number int) Snapshot {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		snap.Err = fmt.Errorf("repo %q", repo)
		return snap
	}
	out, err := runGH(ctx, "api", "graphql",
		"-f", "query="+prViewQuery,
		"-f", "owner="+owner,
		"-f", "name="+name,
		"-F", fmt.Sprintf("number=%d", number),
	)
	if err != nil {
		snap.Err = err
		return snap
	}
	var resp struct {
		Data struct {
			Repository struct {
				PullRequest *prView `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		snap.Err = fmt.Errorf("decode pr graphql: %w", err)
		return snap
	}
	if resp.Data.Repository.PullRequest == nil {
		msg := "pull request not found"
		if len(resp.Errors) > 0 && resp.Errors[0].Message != "" {
			msg = resp.Errors[0].Message
		}
		snap.Err = fmt.Errorf("github: %s", msg)
		return snap
	}
	return applyPRView(snap, *resp.Data.Repository.PullRequest)
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
	snap.UnresolvedComments = countUnresolved(view)
	if snap.Repo == "" {
		snap.Repo = RepoFromRef(view.URL)
	}
	if snap.FetchedAt.IsZero() {
		snap.FetchedAt = time.Now()
	}
	return snap
}

func countUnresolved(view prView) int {
	n := 0
	for _, t := range view.ReviewThreads.Nodes {
		if !t.IsResolved {
			n++
		}
	}
	return n
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
