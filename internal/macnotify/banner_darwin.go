//go:build darwin

package macnotify

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func readBanners(ctx context.Context) ([]Note, error) {
	script := `tell application "System Events"
	if not (exists process "NotificationCenter") then return ""
	tell process "NotificationCenter"
		set out to ""
		repeat with w in windows
			try
				set chunk to ""
				repeat with t in static texts of w
					set chunk to chunk & (value of t as text) & linefeed
				end repeat
				if chunk is not "" then set out to out & chunk & "---" & linefeed
			end try
		end repeat
		return out
	end tell
end tell`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var notes []Note
	now := time.Now()
	for _, block := range strings.Split(string(out), "---") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		var cleaned []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				cleaned = append(cleaned, line)
			}
		}
		if len(cleaned) == 0 {
			continue
		}
		n := Note{Bundle: "com.tinyspeck.slackmacgap", Delivered: now}
		n.Title = cleaned[0]
		if len(cleaned) > 1 {
			n.Subtitle = cleaned[1]
		}
		if len(cleaned) > 2 {
			n.Body = strings.Join(cleaned[2:], "\n")
		}
		if !looksLikeSlackBanner(n) {
			continue
		}
		notes = append(notes, n)
	}
	return notes, nil
}

func looksLikeSlackBanner(n Note) bool {
	blob := strings.ToLower(n.Text())
	return strings.Contains(blob, "slack") || strings.Contains(blob, "jira") || issueKeyPattern.MatchString(n.Text())
}
