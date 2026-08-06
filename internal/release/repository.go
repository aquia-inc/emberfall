package release

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var (
	stableVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	versionTagPattern    = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	remoteNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// GitRepository implements Repository against a local Git working tree.
type GitRepository struct {
	directory string
}

// NewGitRepository returns a repository adapter rooted at directory.
func NewGitRepository(directory string) *GitRepository {
	return &GitRepository{directory: directory}
}

// LatestVersionTag returns the highest reachable stable semantic-version tag.
func (r *GitRepository) LatestVersionTag(ctx context.Context) (string, error) {
	output, err := r.run(ctx, "for-each-ref", "--merged=HEAD", "--sort=-version:refname", "--format=%(refname:short)", "refs/tags")
	if err != nil {
		return "", err
	}
	for _, tag := range strings.Fields(output) {
		if versionTagPattern.MatchString(tag) {
			return tag, nil
		}
	}
	return "", errors.New("no reachable semantic version tag")
}

// CommitsSince returns commits after a reachable baseline tag, newest first.
func (r *GitRepository) CommitsSince(ctx context.Context, baseline string) ([]Commit, error) {
	if err := r.requireReachable(ctx, baseline); err != nil {
		return nil, err
	}
	output, err := r.runRaw(ctx, "log", "-z", "--format=%H%x00%s%x00%b", baseline+"..HEAD")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(output, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%3 != 0 {
		return nil, fmt.Errorf("parse git log records for %q", baseline)
	}
	commits := make([]Commit, 0, len(fields)/3)
	for index := 0; index < len(fields); index += 3 {
		commits = append(commits, Commit{
			Hash:    fields[index],
			Subject: fields[index+1],
			Body:    strings.TrimSuffix(fields[index+2], "\n"),
		})
	}
	return commits, nil
}

// EnsureClean rejects any tracked, staged, or untracked working-tree change.
func (r *GitRepository) EnsureClean(ctx context.Context) error {
	output, err := r.run(ctx, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if output != "" {
		return errors.New("repository has uncommitted changes")
	}
	return nil
}

// CurrentBranch returns the checked-out branch name.
func (r *GitRepository) CurrentBranch(ctx context.Context) (string, error) {
	return r.run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
}

// TagExists reports whether tag already exists locally.
func (r *GitRepository) TagExists(ctx context.Context, tag string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/tags/"+tag)
	command.Dir = r.directory
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git show-ref %q: %w", tag, err)
}

// Commit stages only paths and creates a commit with message.
func (r *GitRepository) Commit(ctx context.Context, message string, paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("commit requires at least one path")
	}
	if _, err := r.run(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return "", err
	}
	arguments := []string{"commit", "--only", "--message", message, "--"}
	arguments = append(arguments, paths...)
	if _, err := r.run(ctx, arguments...); err != nil {
		return "", err
	}
	return r.run(ctx, "rev-parse", "HEAD")
}

// Tag creates tag without replacing an existing tag.
func (r *GitRepository) Tag(ctx context.Context, tag string) error {
	exists, err := r.TagExists(ctx, tag)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tag %q already exists", tag)
	}
	_, err = r.run(ctx, "tag", tag)
	return err
}

// PushAtomic pushes branch and tag together without force updates.
func (r *GitRepository) PushAtomic(ctx context.Context, remote, branch, tag string) error {
	if !validRemoteName(remote) {
		return fmt.Errorf("invalid remote name %q", remote)
	}
	if _, err := r.run(ctx, "remote", "get-url", remote); err != nil {
		return fmt.Errorf("invalid remote name %q: %w", remote, err)
	}
	branchCommit, err := r.run(ctx, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return err
	}
	tagCommit, err := r.run(ctx, "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}")
	if err != nil {
		return err
	}
	if branchCommit != tagCommit {
		return fmt.Errorf("branch %q and tag %q must resolve to the same commit", branch, tag)
	}
	_, err = r.run(ctx, "push", "--atomic", "--", remote,
		"refs/heads/"+branch+":refs/heads/"+branch,
		"refs/tags/"+tag+":refs/tags/"+tag)
	return err
}

func validRemoteName(remote string) bool {
	return remoteNamePattern.MatchString(remote) &&
		!strings.Contains(remote, "..") &&
		!strings.Contains(remote, "@{") &&
		!strings.Contains(remote, "//") &&
		!strings.HasSuffix(remote, "/")
}

func (r *GitRepository) requireReachable(ctx context.Context, tag string) error {
	exists, err := r.TagExists(ctx, tag)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("baseline tag %q does not exist", tag)
	}
	if _, err := r.run(ctx, "merge-base", "--is-ancestor", tag, "HEAD"); err != nil {
		return fmt.Errorf("baseline tag %q is not reachable from HEAD: %w", tag, err)
	}
	return nil
}

func (r *GitRepository) run(ctx context.Context, arguments ...string) (string, error) {
	output, err := r.runRaw(ctx, arguments...)
	return strings.TrimSpace(output), err
}

func (r *GitRepository) runRaw(ctx context.Context, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = r.directory
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
