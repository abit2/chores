//go:build darwin

package macnotify

import "context"

func slackDarwin(ctx context.Context) ([]Note, error) {
	notes, err := readCenter(ctx)
	if err == nil {
		return notes, nil
	}
	banners, bannerErr := readBanners(ctx)
	if bannerErr == nil && len(banners) > 0 {
		return banners, nil
	}
	return nil, err
}
