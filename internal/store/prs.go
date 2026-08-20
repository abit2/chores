package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abit2/chores/internal/ghpr"
)

// PRRecord is a saved GitHub PR or Actions snapshot plus last refresh time.
type PRRecord struct {
	Kind               ghpr.Kind    `json:"kind"`
	Input              string       `json:"input,omitempty"`
	Number             int          `json:"number,omitempty"`
	RunID              int64        `json:"runId,omitempty"`
	Title              string       `json:"title,omitempty"`
	URL                string       `json:"url,omitempty"`
	State              string       `json:"state,omitempty"`
	IsDraft            bool         `json:"isDraft,omitempty"`
	HeadRefName        string       `json:"headRefName,omitempty"`
	Author             string       `json:"author,omitempty"`
	UnresolvedComments int          `json:"unresolvedComments,omitempty"`
	Repo               string       `json:"repo,omitempty"`
	WorkflowName       string       `json:"workflowName,omitempty"`
	Event              string       `json:"event,omitempty"`
	Checks             []ghpr.Check `json:"checks,omitempty"`
	Error              string       `json:"error,omitempty"`
	RefreshedAt        time.Time    `json:"refreshedAt"`
}

// PRsPath is the JSON file of last-fetched GitHub PR / Actions data.
func PRsPath() (string, error) {
	if p := os.Getenv("CHORES_PR_FILE"); p != "" {
		return p, nil
	}
	watch, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(watch), "prs.json"), nil
}

// LoadPRs reads saved GitHub snapshots. A missing file is an empty map.
func LoadPRs() (map[string]PRRecord, error) {
	path, err := PRsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]PRRecord{}, nil
		}
		return nil, err
	}
	var cache map[string]PRRecord
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cache == nil {
		cache = map[string]PRRecord{}
	}
	return cache, nil
}

// SavePRs merges snapshots into prs.json, keyed by repo#number or Actions run.
func SavePRs(snaps []ghpr.Snapshot) error {
	path, err := PRsPath()
	if err != nil {
		return err
	}
	cache, err := LoadPRs()
	if err != nil {
		return err
	}
	changed := false
	for _, snap := range snaps {
		if snap.Kind == ghpr.KindJira {
			continue
		}
		key := PRKey(snap)
		if key == "" {
			continue
		}
		cache[key] = recordFrom(snap)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LookupPR finds a cached GitHub snapshot for the given item.
func LookupPR(cache map[string]PRRecord, snap ghpr.Snapshot) (PRRecord, bool) {
	seen := map[string]struct{}{}
	for _, key := range prKeys(snap) {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if rec, ok := cache[key]; ok {
			return rec, true
		}
	}
	return PRRecord{}, false
}

// PRKey is the canonical cache key for a GitHub PR or Actions run.
func PRKey(s ghpr.Snapshot) string {
	if s.Kind == ghpr.KindJira {
		return ""
	}
	if s.Kind == ghpr.KindRun || s.RunID > 0 || ghpr.IsRunRef(s.Input) {
		if s.Repo != "" && s.RunID > 0 {
			return fmt.Sprintf("%s/actions/runs/%d", s.Repo, s.RunID)
		}
		if s.URL != "" {
			return strings.TrimRight(s.URL, "/")
		}
		return s.Input
	}
	repo, n := ghpr.Identity(s)
	if repo != "" && n > 0 {
		return fmt.Sprintf("%s#%d", repo, n)
	}
	if s.URL != "" {
		return strings.TrimRight(s.URL, "/")
	}
	return s.Input
}

func prKeys(s ghpr.Snapshot) []string {
	keys := []string{PRKey(s)}
	if s.URL != "" {
		keys = append(keys, strings.TrimRight(s.URL, "/"))
	}
	if s.Input != "" {
		keys = append(keys, s.Input)
	}
	return keys
}

func recordFrom(s ghpr.Snapshot) PRRecord {
	rec := PRRecord{
		Kind:               s.Kind,
		Input:              s.Input,
		Number:             s.Number,
		RunID:              s.RunID,
		Title:              s.Title,
		URL:                s.URL,
		State:              s.State,
		IsDraft:            s.IsDraft,
		HeadRefName:        s.HeadRefName,
		Author:             s.Author,
		UnresolvedComments: s.UnresolvedComments,
		Repo:               s.Repo,
		WorkflowName:       s.WorkflowName,
		Event:              s.Event,
		Checks:             s.Checks,
		RefreshedAt:        s.FetchedAt,
	}
	if rec.Kind == "" {
		if rec.RunID > 0 {
			rec.Kind = ghpr.KindRun
		} else {
			rec.Kind = ghpr.KindPR
		}
	}
	if !rec.RefreshedAt.IsZero() {
		rec.RefreshedAt = rec.RefreshedAt.UTC()
	}
	if s.Err != nil {
		rec.Error = s.Err.Error()
	}
	return rec
}

// Snapshot rebuilds a live snapshot from a saved record.
func (r PRRecord) Snapshot() ghpr.Snapshot {
	s := ghpr.Snapshot{
		Kind:               r.Kind,
		Input:              r.Input,
		Number:             r.Number,
		RunID:              r.RunID,
		Title:              r.Title,
		URL:                r.URL,
		State:              r.State,
		IsDraft:            r.IsDraft,
		HeadRefName:        r.HeadRefName,
		Author:             r.Author,
		UnresolvedComments: r.UnresolvedComments,
		Repo:               r.Repo,
		WorkflowName:       r.WorkflowName,
		Event:              r.Event,
		Checks:             r.Checks,
		FetchedAt:          r.RefreshedAt,
	}
	if r.Error != "" {
		s.Err = errors.New(r.Error)
	}
	return s
}
