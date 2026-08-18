package jira

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abit2/chores/internal/macnotify"
)

// Entry is one Slack/Jira desktop notification saved for an issue key.
type Entry struct {
	Received time.Time `json:"received,omitempty"`
	Title    string    `json:"title,omitempty"`
	Subtitle string    `json:"subtitle,omitempty"`
	Body     string    `json:"body,omitempty"`
	Bundle   string    `json:"bundle,omitempty"`
}

type noteFile struct {
	Issues     map[string][]Entry   `json:"issues"`
	Cleared    map[string]time.Time `json:"cleared,omitempty"`
	ClearedAll time.Time            `json:"clearedAll,omitempty"`
}

// NotesPath is the JSON file of Slack/Jira notifications grouped by issue key.
func NotesPath() (string, error) {
	if p := os.Getenv("CHORES_NOTES_FILE"); p != "" {
		return p, nil
	}
	if w := os.Getenv("CHORES_WATCH_FILE"); w != "" {
		return filepath.Join(filepath.Dir(w), "notifications.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "chores", "notifications.json"), nil
}

func recordNotes(notes []macnotify.Note) {
	if len(notes) == 0 {
		return
	}
	_ = appendNotes(notes)
}

func appendNotes(notes []macnotify.Note) error {
	path, err := NotesPath()
	if err != nil {
		return err
	}
	log, err := loadNoteFile(path)
	if err != nil {
		return err
	}
	changed := false
	for _, n := range notes {
		keys := macnotify.IssueKeys(n.Text())
		if len(keys) == 0 {
			continue
		}
		e := entryFrom(n)
		for _, key := range keys {
			if log.dropped(key, e.Received) {
				continue
			}
			if hasEntry(log.Issues[key], e) {
				continue
			}
			log.Issues[key] = insertReceived(log.Issues[key], e)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := saveNoteFile(path, log); err != nil {
		return err
	}
	noteLog = log
	return nil
}

// ClearKey deletes saved notifications for one issue and ignores older Slack banners.
func ClearKey(key string) error {
	key = strings.ToUpper(strings.TrimSpace(key))
	if !keyRe.MatchString(key) {
		return fmt.Errorf("not a Jira key: %s", key)
	}
	return mutateNoteFile(func(log *noteFile) {
		delete(log.Issues, key)
		if log.Cleared == nil {
			log.Cleared = map[string]time.Time{}
		}
		log.Cleared[key] = time.Now().UTC()
	})
}

// ClearAll deletes every saved Jira notification.
func ClearAll() error {
	return mutateNoteFile(func(log *noteFile) {
		log.Issues = map[string][]Entry{}
		log.ClearedAll = time.Now().UTC()
	})
}

func mutateNoteFile(fn func(*noteFile)) error {
	path, err := NotesPath()
	if err != nil {
		return err
	}
	noteMu.Lock()
	defer noteMu.Unlock()
	log, err := loadNoteFile(path)
	if err != nil {
		return err
	}
	fn(&log)
	if err := saveNoteFile(path, log); err != nil {
		return err
	}
	noteLog = log
	noteAt = time.Time{}
	return nil
}

func (f noteFile) dropped(key string, delivered time.Time) bool {
	if key == "" {
		return false
	}
	if !f.ClearedAll.IsZero() && !delivered.After(f.ClearedAll) {
		return true
	}
	if t, ok := f.Cleared[strings.ToUpper(key)]; ok && !delivered.After(t) {
		return true
	}
	return false
}

func isDropped(key string, delivered time.Time) bool {
	noteMu.Lock()
	defer noteMu.Unlock()
	return noteLog.dropped(key, delivered)
}

func entryFrom(n macnotify.Note) Entry {
	return Entry{
		Received: n.Delivered.UTC(),
		Title:    n.Title,
		Subtitle: n.Subtitle,
		Body:     n.Body,
		Bundle:   n.Bundle,
	}
}

func hasEntry(list []Entry, e Entry) bool {
	for _, existing := range list {
		if existing.Received.Equal(e.Received) &&
			existing.Title == e.Title &&
			existing.Subtitle == e.Subtitle &&
			existing.Body == e.Body {
			return true
		}
	}
	return false
}

func insertReceived(list []Entry, e Entry) []Entry {
	for i, existing := range list {
		if existing.Received.After(e.Received) {
			list = append(list, Entry{})
			copy(list[i+1:], list[i:])
			list[i] = e
			return list
		}
	}
	return append(list, e)
}

func loadNoteFile(path string) (noteFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyNoteFile(), nil
		}
		return noteFile{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return noteFile{}, err
	}
	if _, ok := raw["issues"]; ok {
		var log noteFile
		if err := json.Unmarshal(b, &log); err != nil {
			return noteFile{}, err
		}
		if log.Issues == nil {
			log.Issues = map[string][]Entry{}
		}
		return log, nil
	}
	var legacy map[string][]Entry
	if err := json.Unmarshal(b, &legacy); err != nil {
		return noteFile{}, err
	}
	if legacy == nil {
		legacy = map[string][]Entry{}
	}
	return noteFile{Issues: legacy}, nil
}

func emptyNoteFile() noteFile {
	return noteFile{
		Issues:  map[string][]Entry{},
		Cleared: map[string]time.Time{},
	}
}

func saveNoteFile(path string, log noteFile) error {
	if log.Issues == nil {
		log.Issues = map[string][]Entry{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(log, "", "  ")
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
