//go:build darwin

package macnotify

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const cocoaEpochUnix = 978307200

var errFullDiskAccess = errors.New("cannot read Notification Center: grant Full Disk Access to this terminal (System Settings → Privacy & Security → Full Disk Access)")

func dbCandidates() []string {
	home, _ := os.UserHomeDir()
	var out []string
	if home != "" {
		out = append(out, filepath.Join(home, "Library/Group Containers/group.com.apple.usernoted/db2/db"))
	}
	if dir, err := darwinUserDir(); err == nil && dir != "" {
		out = append(out, filepath.Join(dir, "com.apple.notificationcenter/db2/db"))
		out = append(out, filepath.Join(dir, "com.apple.notificationcenter/db/db"))
	}
	return out
}

func darwinUserDir() (string, error) {
	cmd := exec.Command("getconf", "DARWIN_USER_DIR")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func readCenter(ctx context.Context) ([]Note, error) {
	var firstErr error
	for _, db := range dbCandidates() {
		notes, err := readDB(ctx, db)
		if err == nil {
			return notes, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = errFullDiskAccess
	}
	return nil, firstErr
}

func readDB(ctx context.Context, db string) ([]Note, error) {
	if _, err := os.Stat(db); err != nil {
		if os.IsPermission(err) {
			return nil, errFullDiskAccess
		}
		return nil, err
	}
	tmp, err := snapshotDB(db)
	if err != nil {
		if os.IsPermission(err) {
			return nil, errFullDiskAccess
		}
		return nil, err
	}
	defer os.RemoveAll(filepath.Dir(tmp))

	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		sqlite = "/usr/bin/sqlite3"
	}
	query := `SELECT a.identifier, IFNULL(r.delivered_date,0), hex(r.data)
FROM record r
JOIN app a ON a.app_id = r.app_id
ORDER BY r.delivered_date DESC
LIMIT 400;`
	cmd := exec.CommandContext(ctx, sqlite, "-readonly", "-noheader", "-separator", "\x1f", tmp, query)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("sqlite3: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}

	var notes []Note
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		bundle := parts[0]
		if !isSlackBundle(bundle) {
			continue
		}
		sec, _ := strconv.ParseFloat(parts[1], 64)
		raw, err := hex.DecodeString(strings.TrimSpace(parts[2]))
		if err != nil || len(raw) == 0 {
			continue
		}
		title, subtitle, body, app := decodeRequest(raw)
		if app != "" && !isSlackBundle(bundle) && isSlackBundle(app) {
			bundle = app
		}
		if title == "" && subtitle == "" && body == "" {
			continue
		}
		notes = append(notes, Note{
			Bundle:    bundle,
			Title:     title,
			Subtitle:  subtitle,
			Body:      body,
			Delivered: cocoaTime(sec),
		})
	}
	return notes, nil
}

func cocoaTime(sec float64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(cocoaEpochUnix+int64(sec), 0)
}

func snapshotDB(src string) (string, error) {
	dir, err := os.MkdirTemp("", "chores-nc-")
	if err != nil {
		return "", err
	}
	srcDir := filepath.Dir(src)
	for _, name := range []string{"db", "db-wal", "db-shm"} {
		from := filepath.Join(srcDir, name)
		to := filepath.Join(dir, name)
		if err := copyFile(from, to); err != nil && !os.IsNotExist(err) {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return filepath.Join(dir, "db"), nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
