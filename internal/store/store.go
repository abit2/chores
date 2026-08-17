package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abit2/chores/internal/ghpr"
)

// Watch is the persisted watch list.
type Watch struct {
	URLs   []string `json:"urls"`
	Repo   string   `json:"repo,omitempty"`
	Hidden []string `json:"hidden,omitempty"`
}

// Path is the JSON file used to remember watched URLs.
func Path() (string, error) {
	if p := os.Getenv("CHORES_WATCH_FILE"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "chores", "watch.json"), nil
}

// Load reads the watch list. A missing file is an empty list, not an error.
func Load() (Watch, error) {
	path, err := Path()
	if err != nil {
		return Watch{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Watch{}, nil
		}
		return Watch{}, err
	}
	var w Watch
	if err := json.Unmarshal(b, &w); err != nil {
		return Watch{}, fmt.Errorf("parse %s: %w", path, err)
	}
	w.URLs = ghpr.ParseRefs(w.URLs)
	w.Hidden = ghpr.ParseRefs(w.Hidden)
	return w, nil
}

// Save writes the watch list, creating the config directory if needed.
func Save(w Watch) error {
	path, err := Path()
	if err != nil {
		return err
	}
	w.URLs = ghpr.ParseRefs(w.URLs)
	w.Hidden = ghpr.ParseRefs(w.Hidden)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

// MergeURLs appends extra refs onto base, dropping duplicates.
func MergeURLs(base, extra []string) []string {
	return ghpr.ParseRefs(append(append([]string{}, base...), extra...))
}
