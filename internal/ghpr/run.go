package ghpr

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type runView struct {
	Attempt      int    `json:"attempt"`
	Conclusion   string `json:"conclusion"`
	DisplayTitle string `json:"displayTitle"`
	Event        string `json:"event"`
	HeadBranch   string `json:"headBranch"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	WorkflowName string `json:"workflowName"`
	Jobs         []struct {
		Name        string `json:"name"`
		Status      string `json:"status"`
		Conclusion  string `json:"conclusion"`
		URL         string `json:"url"`
		StartedAt   string `json:"startedAt"`
		CompletedAt string `json:"completedAt"`
	} `json:"jobs"`
}

type runListItem struct {
	DatabaseID   int64  `json:"databaseId"`
	Conclusion   string `json:"conclusion"`
	DisplayTitle string `json:"displayTitle"`
	Event        string `json:"event"`
	HeadBranch   string `json:"headBranch"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	WorkflowName string `json:"workflowName"`
	StartedAt    string `json:"startedAt"`
}

// FetchRun loads a GitHub Actions workflow run and its jobs.
func FetchRun(ctx context.Context, ref string) Snapshot {
	snap := Snapshot{
		Kind:      KindRun,
		Input:     ref,
		Repo:      RepoFromRef(ref),
		RunID:     RunIDFromRef(ref),
		FetchedAt: time.Now(),
	}
	if snap.RunID == 0 {
		snap.Err = fmt.Errorf("not a GitHub Actions run URL: %s", ref)
		return snap
	}
	return loadRun(ctx, snap)
}

func loadRun(ctx context.Context, snap Snapshot) Snapshot {
	snap.Kind = KindRun
	snap.Err = nil
	snap.FetchedAt = time.Now()
	args := []string{
		"run", "view", strconv.FormatInt(snap.RunID, 10),
		"--json", "attempt,conclusion,displayTitle,event,headBranch,jobs,name,status,url,workflowName",
	}
	if snap.Repo != "" {
		args = append(args, "--repo", snap.Repo)
	}
	out, err := runGH(ctx, args...)
	if err != nil {
		snap.Err = err
		return snap
	}
	var view runView
	if err := json.Unmarshal(out, &view); err != nil {
		snap.Err = fmt.Errorf("decode run view: %w", err)
		return snap
	}
	applyRunView(&snap, view)
	return snap
}

func applyRunView(snap *Snapshot, view runView) {
	snap.URL = view.URL
	snap.Title = view.DisplayTitle
	if snap.Title == "" {
		snap.Title = view.Name
	}
	snap.WorkflowName = view.WorkflowName
	if snap.WorkflowName == "" {
		snap.WorkflowName = view.Name
	}
	snap.Event = view.Event
	snap.HeadRefName = view.HeadBranch
	snap.State = runState(view.Status, view.Conclusion)
	snap.Checks = make([]Check, 0, len(view.Jobs)+1)
	for _, job := range view.Jobs {
		snap.Checks = append(snap.Checks, Check{
			Name:        job.Name,
			State:       runState(job.Status, job.Conclusion),
			Bucket:      jobBucket(job.Status, job.Conclusion),
			Workflow:    snap.WorkflowName,
			Link:        job.URL,
			StartedAt:   job.StartedAt,
			CompletedAt: job.CompletedAt,
		})
	}
	if len(snap.Checks) == 0 {
		snap.Checks = []Check{{
			Name:     snap.WorkflowName,
			State:    snap.State,
			Bucket:   jobBucket(view.Status, view.Conclusion),
			Workflow: snap.WorkflowName,
			Link:     snap.URL,
		}}
	}
}

// ListRuns returns recent workflow runs for a repository.
func ListRuns(ctx context.Context, repo string, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = 15
	}
	args := []string{
		"run", "list", "--repo", repo, "--limit", strconv.Itoa(limit),
		"--json", "databaseId,status,conclusion,name,workflowName,url,headBranch,displayTitle,event,startedAt",
	}
	out, err := runGH(ctx, args...)
	if err != nil {
		return nil, err
	}
	var items []runListItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("decode run list: %w", err)
	}
	snaps := make([]Snapshot, 0, len(items))
	for _, item := range items {
		snaps = append(snaps, snapshotFromList(repo, item))
	}
	return snaps, nil
}

func snapshotFromList(repo string, item runListItem) Snapshot {
	title := item.DisplayTitle
	if title == "" {
		title = item.Name
	}
	workflow := item.WorkflowName
	if workflow == "" {
		workflow = item.Name
	}
	state := runState(item.Status, item.Conclusion)
	return Snapshot{
		Kind:         KindRun,
		Input:        item.URL,
		RunID:        item.DatabaseID,
		Title:        title,
		URL:          item.URL,
		State:        state,
		HeadRefName:  item.HeadBranch,
		Repo:         repo,
		WorkflowName: workflow,
		Event:        item.Event,
		Checks: []Check{{
			Name:      workflow,
			State:     state,
			Bucket:    jobBucket(item.Status, item.Conclusion),
			Workflow:  workflow,
			Link:      item.URL,
			StartedAt: item.StartedAt,
		}},
		FetchedAt: time.Now(),
	}
}

func runState(status, conclusion string) string {
	if strings.EqualFold(status, "completed") && conclusion != "" {
		return strings.ToUpper(conclusion)
	}
	if status != "" {
		return strings.ToUpper(status)
	}
	return strings.ToUpper(conclusion)
}

func jobBucket(status, conclusion string) string {
	if !strings.EqualFold(status, "completed") {
		return "pending"
	}
	switch strings.ToLower(conclusion) {
	case "success":
		return "pass"
	case "failure", "timed_out", "startup_failure", "action_required":
		return "fail"
	case "cancelled":
		return "cancel"
	case "skipped", "neutral":
		return "skipping"
	default:
		return "pending"
	}
}
