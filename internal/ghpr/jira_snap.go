package ghpr

import (
	"strings"

	"github.com/abit2/chores/internal/jira"
)

func snapshotFromJira(issue jira.Issue) Snapshot {
	return Snapshot{
		Kind:          KindJira,
		Input:         issue.Input,
		Title:         issue.Title,
		URL:           issue.URL,
		State:         issue.Status,
		Author:        issue.Assignee,
		Repo:          issue.Host,
		IssueKey:      issue.Key,
		IssueType:     issue.Type,
		Priority:      issue.Priority,
		Updated:       issue.Updated,
		LastCommenter: issue.LastCommenter,
		Err:           issue.Err,
		FetchedAt:     issue.FetchedAt,
	}
}

func isJiraSnap(s Snapshot) bool {
	if s.Kind == KindJira || s.IssueKey != "" {
		return true
	}
	if strings.HasPrefix(s.Input, "slack:") {
		return true
	}
	return jira.IsRef(s.Input) || jira.IsRef(s.URL)
}

func sameJira(a, b Snapshot) bool {
	ka, kb := jiraKey(a), jiraKey(b)
	if ka != "" && kb != "" {
		if !strings.EqualFold(ka, kb) {
			return false
		}
		ha, hb := jiraHost(a), jiraHost(b)
		if ha != "" && hb != "" && !strings.EqualFold(ha, hb) {
			return false
		}
		return true
	}
	if a.Input != "" && a.Input == b.Input {
		return true
	}
	if a.URL != "" && b.URL != "" && strings.TrimRight(a.URL, "/") == strings.TrimRight(b.URL, "/") {
		return true
	}
	return false
}

func jiraKey(s Snapshot) string {
	if s.IssueKey != "" {
		return s.IssueKey
	}
	if ref, ok := jira.Parse(s.URL); ok {
		return ref.Key
	}
	if ref, ok := jira.Parse(s.Input); ok {
		return ref.Key
	}
	return ""
}

func jiraHost(s Snapshot) string {
	if s.Kind == KindJira && s.Repo != "" {
		return s.Repo
	}
	if ref, ok := jira.Parse(s.URL); ok && ref.Host != "" {
		return ref.Host
	}
	if ref, ok := jira.Parse(s.Input); ok && ref.Host != "" {
		return ref.Host
	}
	return ""
}
