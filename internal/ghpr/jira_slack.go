package ghpr

import (
	"context"

	"github.com/abit2/chores/internal/jira"
)

// ListJiraSlack discovers Jira issues from Slack desktop notifications.
func ListJiraSlack(ctx context.Context, known []Snapshot) ([]Snapshot, error) {
	keys := make(map[string]bool, len(known))
	for _, s := range known {
		if k := jiraKey(s); k != "" {
			keys[k] = true
		}
		if s.Input != "" {
			keys[s.Input] = true
		}
	}
	issues, err := jira.Discover(ctx, keys)
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(issues))
	for _, issue := range issues {
		out = append(out, snapshotFromJira(issue))
	}
	return out, nil
}
