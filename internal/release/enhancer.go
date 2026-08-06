package release

import (
	"context"
	"errors"
	"strings"
)

// ReleaseNotesUpdater coordinates the best-effort enhancement of an existing
// GitHub Release. A failed enhancement returns deterministic notes and never
// patches the release.
type ReleaseNotesUpdater struct {
	Releases *GitHubReleaseClient
	Enhancer Enhancer
}

// EnhanceRelease updates the matching GitHub Release only after enhancement
// succeeds. It returns the unchanged deterministic notes for any failure.
func (u ReleaseNotesUpdater) EnhanceRelease(ctx context.Context, input EnhancementInput) (string, error) {
	if u.Releases == nil || u.Enhancer == nil {
		return input.Notes, errors.New("release notes updater is not configured")
	}
	release, err := u.Releases.ReleaseByTag(ctx, input.Tag)
	if err != nil {
		return input.Notes, err
	}
	if strings.Contains(release.Body, notesEnhancementMarker) {
		return release.Body, nil
	}
	enhanced, err := u.Enhancer.Enhance(ctx, input)
	if err != nil {
		return input.Notes, err
	}
	if err := u.Releases.UpdateReleaseBody(ctx, release.ID, enhanced); err != nil {
		return input.Notes, err
	}
	return enhanced, nil
}
