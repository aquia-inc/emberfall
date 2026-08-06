package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var releasePaths = []string{"cmd/root.go", "tests/cli.bats", "CHANGELOG.md"}

// Publisher creates and atomically publishes prepared release changes.
type Publisher struct {
	repository *GitRepository
	remote     string
}

// NewPublisher returns a publisher that pushes to remote.
func NewPublisher(repository *GitRepository, remote string) *Publisher {
	return &Publisher{repository: repository, remote: remote}
}

// Publish commits the prepared version files and changelog, tags the commit,
// and pushes main and the tag as one non-force transaction.
func (p *Publisher) Publish(ctx context.Context, version string) error {
	if p == nil || p.repository == nil {
		return errors.New("publisher requires a repository")
	}
	if !stableVersionPattern.MatchString(version) {
		return fmt.Errorf("version %q must be stable X.Y.Z", version)
	}
	branch, err := p.repository.CurrentBranch(ctx)
	if err != nil {
		return err
	}
	if branch != "main" {
		return fmt.Errorf("release publication requires main branch, got %q", branch)
	}
	tag := "v" + version
	exists, err := p.repository.TagExists(ctx, tag)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("release tag %q already exists", tag)
	}
	if err := p.requirePreparedPaths(ctx); err != nil {
		return err
	}
	message := "chore(release): bump version to " + version
	if _, err := p.repository.Commit(ctx, message, releasePaths...); err != nil {
		return err
	}
	if err := p.repository.Tag(ctx, tag); err != nil {
		return err
	}
	return p.repository.PushAtomic(ctx, p.remote, "main", tag)
}

func (p *Publisher) requirePreparedPaths(ctx context.Context) error {
	output, err := p.repository.runRaw(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	allowed := make(map[string]bool, len(releasePaths))
	for _, path := range releasePaths {
		allowed[path] = true
	}
	changed := make(map[string]bool, len(releasePaths))
	entries := strings.Split(output, "\x00")
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if entry == "" {
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			return fmt.Errorf("unexpected git status entry %q", entry)
		}
		path := entry[3:]
		if err := requireAllowedPath(path, allowed); err != nil {
			return err
		}
		changed[path] = true
		if entry[0] == 'R' || entry[0] == 'C' || entry[1] == 'R' || entry[1] == 'C' {
			index++
			if index >= len(entries) || entries[index] == "" {
				return fmt.Errorf("rename or copy status for %q is missing its source path", path)
			}
			source := entries[index]
			if err := requireAllowedPath(source, allowed); err != nil {
				return err
			}
			changed[source] = true
		}
	}
	for _, path := range releasePaths {
		if !changed[path] {
			return fmt.Errorf("prepared release is missing change to %q", path)
		}
	}
	return nil
}

func requireAllowedPath(path string, allowed map[string]bool) error {
	if !allowed[path] {
		return fmt.Errorf("unmanaged path %q", path)
	}
	return nil
}
