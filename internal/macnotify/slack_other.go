//go:build !darwin

package macnotify

import "context"

func slackDarwin(ctx context.Context) ([]Note, error) {
	return nil, nil
}
