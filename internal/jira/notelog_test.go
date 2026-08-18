package jira

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abit2/chores/internal/macnotify"
)

func resetNoteState() {
	noteMu.Lock()
	defer noteMu.Unlock()
	noteLog = emptyNoteFile()
	noteCache = nil
	noteAt = time.Time{}
	noteErr = nil
}

func TestAppendNotesGroupedByKeyInReceivedOrder(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	path := filepath.Join(t.TempDir(), "notifications.json")
	t.Setenv("CHORES_NOTES_FILE", path)

	later := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	earlier := later.Add(-time.Hour)
	err := appendNotes([]macnotify.Note{
		{Title: "Jira", Body: "commented on UI-1234 later", Delivered: later},
		{Title: "Jira", Body: "assigned UI-1234 first", Delivered: earlier},
		{Title: "Jira", Body: "also WRBOT-9", Delivered: later},
	})
	if err != nil {
		t.Fatal(err)
	}

	log := readNotes(t, path)
	if len(log["UI-1234"]) != 2 {
		t.Fatalf("UI-1234=%+v", log["UI-1234"])
	}
	if log["UI-1234"][0].Body != "assigned UI-1234 first" {
		t.Fatalf("want oldest first, got %+v", log["UI-1234"])
	}
	if log["UI-1234"][1].Body != "commented on UI-1234 later" {
		t.Fatalf("want newest last, got %+v", log["UI-1234"])
	}
	if len(log["WRBOT-9"]) != 1 || log["WRBOT-9"][0].Body != "also WRBOT-9" {
		t.Fatalf("WRBOT-9=%+v", log["WRBOT-9"])
	}
}

func TestAppendNotesDedupsAndKeepsHistory(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	path := filepath.Join(t.TempDir(), "notifications.json")
	t.Setenv("CHORES_NOTES_FILE", path)

	first := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	n1 := macnotify.Note{Title: "Jira", Body: "assigned UI-1", Delivered: first}
	if err := appendNotes([]macnotify.Note{n1}); err != nil {
		t.Fatal(err)
	}
	if err := appendNotes([]macnotify.Note{n1}); err != nil {
		t.Fatal(err)
	}
	n2 := macnotify.Note{Title: "Jira", Body: "commented UI-1", Delivered: first.Add(time.Minute)}
	if err := appendNotes([]macnotify.Note{n1, n2}); err != nil {
		t.Fatal(err)
	}

	log := readNotes(t, path)
	if len(log["UI-1"]) != 2 {
		t.Fatalf("UI-1=%+v", log["UI-1"])
	}
}

func TestAppendNotesSkipsUnkeyed(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	path := filepath.Join(t.TempDir(), "notifications.json")
	t.Setenv("CHORES_NOTES_FILE", path)

	if err := appendNotes([]macnotify.Note{
		{Title: "Slack", Body: "standup in 5"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no file, err=%v", err)
	}
}

func TestNotesPathBesideWatchFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHORES_NOTES_FILE", "")
	t.Setenv("CHORES_WATCH_FILE", filepath.Join(dir, "watch.json"))
	got, err := NotesPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "notifications.json") {
		t.Fatalf("got %s", got)
	}
}

func TestClearKeyDropsOldNotes(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	path := filepath.Join(t.TempDir(), "notifications.json")
	t.Setenv("CHORES_NOTES_FILE", path)

	old := time.Now().UTC().Add(-time.Minute)
	if err := appendNotes([]macnotify.Note{
		{Title: "Jira", Body: "assigned UI-1234", Delivered: old},
		{Title: "Jira", Body: "also WRBOT-9", Delivered: old},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ClearKey("ui-1234"); err != nil {
		t.Fatal(err)
	}
	log := readNoteFile(t, path)
	if _, ok := log.Issues["UI-1234"]; ok {
		t.Fatalf("UI-1234 still present: %+v", log.Issues)
	}
	if len(log.Issues["WRBOT-9"]) != 1 {
		t.Fatalf("WRBOT-9=%+v", log.Issues["WRBOT-9"])
	}

	if err := appendNotes([]macnotify.Note{
		{Title: "Jira", Body: "assigned UI-1234", Delivered: old},
	}); err != nil {
		t.Fatal(err)
	}
	log = readNoteFile(t, path)
	if _, ok := log.Issues["UI-1234"]; ok {
		t.Fatal("cleared key came back from old notification")
	}

	newer := time.Now().UTC().Add(time.Second)
	if err := appendNotes([]macnotify.Note{
		{Title: "Jira", Body: "commented UI-1234 later", Delivered: newer},
	}); err != nil {
		t.Fatal(err)
	}
	log = readNoteFile(t, path)
	if len(log.Issues["UI-1234"]) != 1 {
		t.Fatalf("new note should save: %+v", log.Issues["UI-1234"])
	}
}

func TestClearAllDropsEverything(t *testing.T) {
	resetNoteState()
	t.Cleanup(resetNoteState)
	path := filepath.Join(t.TempDir(), "notifications.json")
	t.Setenv("CHORES_NOTES_FILE", path)
	old := time.Now().UTC().Add(-time.Minute)
	if err := appendNotes([]macnotify.Note{
		{Title: "Jira", Body: "UI-1 hello", Delivered: old},
		{Title: "Jira", Body: "UI-2 hello", Delivered: old},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ClearAll(); err != nil {
		t.Fatal(err)
	}
	log := readNoteFile(t, path)
	if len(log.Issues) != 0 {
		t.Fatalf("issues=%+v", log.Issues)
	}
	if err := appendNotes([]macnotify.Note{
		{Title: "Jira", Body: "UI-1 hello", Delivered: old},
	}); err != nil {
		t.Fatal(err)
	}
	log = readNoteFile(t, path)
	if len(log.Issues) != 0 {
		t.Fatal("old notes came back after clear all")
	}
}

func TestLoadLegacyNotesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	legacy := `{
  "UI-1": [
    {"received": "2026-08-17T10:00:00Z", "body": "hello UI-1"}
  ]
}
`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	log, err := loadNoteFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Issues["UI-1"]) != 1 || log.Issues["UI-1"][0].Body != "hello UI-1" {
		t.Fatalf("%+v", log.Issues)
	}
}

func readNotes(t *testing.T, path string) map[string][]Entry {
	t.Helper()
	return readNoteFile(t, path).Issues
}

func readNoteFile(t *testing.T, path string) noteFile {
	t.Helper()
	log, err := loadNoteFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return log
}
