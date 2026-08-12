package release

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitRepositoryLatestVersionTagUsesReachableAnnotatedAndLightweightTags(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "first")
	repo.git(t, "tag", "-a", "v1.2.0", "-m", "v1.2.0")
	repo.commit(t, "second")
	repo.git(t, "tag", "v1.3.0")
	repo.git(t, "checkout", "-b", "side", "HEAD~1")
	repo.commit(t, "side change")
	repo.git(t, "tag", "v9.0.0")
	repo.git(t, "checkout", "main")

	got, err := NewGitRepository(repo.dir).LatestVersionTag(context.Background())
	if err != nil {
		t.Fatalf("LatestVersionTag: %v", err)
	}
	if got != "v1.3.0" {
		t.Fatalf("LatestVersionTag = %q, want v1.3.0", got)
	}
}

func TestGitRepositoryLatestVersionTagRejectsMissingBaseline(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "first")

	_, err := NewGitRepository(repo.dir).LatestVersionTag(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no reachable semantic version tag") {
		t.Fatalf("LatestVersionTag error = %v, want missing baseline", err)
	}
}

func TestGitRepositoryCommitsSinceReturnsOnlyRange(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "baseline")
	repo.git(t, "tag", "v1.0.0")
	repo.commit(t, "first release change")
	repo.commit(t, "second release change")
	body := "first body line\nsecond body line with unit \x1f separator\n\nRefs: #42 with record \x1e separator"
	repo.commitMessage(t, "feat: preserve full commit input", body)

	commits, err := NewGitRepository(repo.dir).CommitsSince(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("CommitsSince length = %d, want 3", len(commits))
	}
	if commits[0].Subject != "feat: preserve full commit input" || commits[1].Subject != "second release change" || commits[2].Subject != "first release change" {
		t.Fatalf("CommitsSince subjects = %#v, want reverse chronological range", commits)
	}
	if commits[0].Hash == "" || commits[0].Body != body {
		t.Fatalf("CommitsSince first commit = %#v, want exact multiline body %q", commits[0], body)
	}
}

func TestGitRepositoryCommitsSinceRejectsUnknownBaseline(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "first")

	_, err := NewGitRepository(repo.dir).CommitsSince(context.Background(), "v1.0.0")
	if err == nil {
		t.Fatal("CommitsSince succeeded for missing baseline")
	}
}

func TestGitRepositoryEnsureCleanRejectsTrackedAndUntrackedChanges(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "first")
	gitRepo := NewGitRepository(repo.dir)
	if err := gitRepo.EnsureClean(context.Background()); err != nil {
		t.Fatalf("EnsureClean clean repository: %v", err)
	}
	repo.write(t, "tracked.txt", "changed\n")
	if err := gitRepo.EnsureClean(context.Background()); err == nil {
		t.Fatal("EnsureClean accepted tracked change")
	}
	repo.git(t, "restore", "tracked.txt")
	repo.write(t, "untracked.txt", "untracked\n")
	if err := gitRepo.EnsureClean(context.Background()); err == nil {
		t.Fatal("EnsureClean accepted untracked file")
	}
}

func TestGitRepositoryCurrentBranchTagExistsCommitAndTag(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "first")
	gitRepo := NewGitRepository(repo.dir)

	branch, err := gitRepo.CurrentBranch(context.Background())
	if err != nil || branch != "main" {
		t.Fatalf("CurrentBranch = %q, %v; want main, nil", branch, err)
	}
	exists, err := gitRepo.TagExists(context.Background(), "v1.0.0")
	if err != nil || exists {
		t.Fatalf("TagExists missing = %v, %v; want false, nil", exists, err)
	}
	repo.write(t, "release.txt", "release\n")
	hash, err := gitRepo.Commit(context.Background(), "chore(release): bump version to 1.0.0", "release.txt")
	if err != nil || hash == "" {
		t.Fatalf("Commit = %q, %v", hash, err)
	}
	if got := repo.git(t, "log", "-1", "--format=%s"); got != "chore(release): bump version to 1.0.0" {
		t.Fatalf("commit subject = %q", got)
	}
	if err := gitRepo.Tag(context.Background(), "v1.0.0"); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	exists, err = gitRepo.TagExists(context.Background(), "v1.0.0")
	if err != nil || !exists {
		t.Fatalf("TagExists created = %v, %v; want true, nil", exists, err)
	}
	if err := gitRepo.Tag(context.Background(), "v1.0.0"); err == nil {
		t.Fatal("Tag replaced existing tag")
	}
}

func TestGitRepositoryPushAtomicPublishesMainAndTag(t *testing.T) {
	repo, remote := newRemoteRepository(t)
	repo.commit(t, "first")
	repo.git(t, "tag", "v1.0.0")

	if err := NewGitRepository(repo.dir).PushAtomic(context.Background(), "origin", "main", "v1.0.0"); err != nil {
		t.Fatalf("PushAtomic: %v", err)
	}
	if got := gitAt(t, remote, "rev-parse", "refs/heads/main"); got == "" {
		t.Fatal("remote main was not published")
	}
	if got := gitAt(t, remote, "rev-parse", "refs/tags/v1.0.0"); got == "" {
		t.Fatal("remote tag was not published")
	}
}

func TestGitRepositoryPushAtomicRejectsNonFastForwardAndCanRetry(t *testing.T) {
	repo, remote := newRemoteRepository(t)
	repo.commit(t, "first")
	repo.git(t, "push", "origin", "main")
	other := t.TempDir()
	mustRun(t, other, "git", "clone", remote, other)
	gitAt(t, other, "config", "user.name", "Test User")
	gitAt(t, other, "config", "user.email", "test@example.com")
	writeFile(t, filepath.Join(other, "other.txt"), "other\n")
	gitAt(t, other, "add", "other.txt")
	gitAt(t, other, "commit", "-m", "remote change")
	gitAt(t, other, "push", "origin", "main")

	repo.commit(t, "local change")
	repo.git(t, "tag", "v1.0.0")
	gitRepo := NewGitRepository(repo.dir)
	if err := gitRepo.PushAtomic(context.Background(), "origin", "main", "v1.0.0"); err == nil {
		t.Fatal("PushAtomic accepted non-fast-forward main")
	}
	if refExists(t, remote, "refs/tags/v1.0.0") {
		t.Fatal("atomic push published tag despite rejected branch")
	}
	repo.git(t, "fetch", "origin", "main")
	repo.git(t, "rebase", "origin/main")
	if err := gitRepo.PushAtomic(context.Background(), "origin", "main", "v1.0.0"); err == nil || !strings.Contains(err.Error(), "same commit") {
		t.Fatalf("PushAtomic stale tag error = %v, want branch/tag mismatch", err)
	}
	repo.git(t, "tag", "--delete", "v1.0.0")
	if err := gitRepo.Tag(context.Background(), "v1.0.0"); err != nil {
		t.Fatalf("recreate reconciled tag: %v", err)
	}
	if err := gitRepo.PushAtomic(context.Background(), "origin", "main", "v1.0.0"); err != nil {
		t.Fatalf("PushAtomic retry: %v", err)
	}
	remoteMain := gitAt(t, remote, "rev-parse", "refs/heads/main^{commit}")
	remoteTag := gitAt(t, remote, "rev-parse", "refs/tags/v1.0.0^{commit}")
	if remoteMain != remoteTag {
		t.Fatalf("remote main = %s, tag = %s; want same release commit", remoteMain, remoteTag)
	}
}

func TestGitRepositoryPushAtomicRejectsRemoteTagCollisionWithoutUpdatingEitherRef(t *testing.T) {
	repo, remote := newRemoteRepository(t)
	repo.commit(t, "initial")
	repo.git(t, "tag", "v1.0.0")
	repo.git(t, "push", "origin", "main", "refs/tags/v1.0.0")
	remoteMainBefore := gitAt(t, remote, "rev-parse", "refs/heads/main^{commit}")
	remoteTagBefore := gitAt(t, remote, "rev-parse", "refs/tags/v1.0.0^{commit}")
	repo.git(t, "tag", "--delete", "v1.0.0")
	repo.commit(t, "release")
	repo.git(t, "tag", "v1.0.0")

	if err := NewGitRepository(repo.dir).PushAtomic(context.Background(), "origin", "main", "v1.0.0"); err == nil {
		t.Fatal("PushAtomic replaced a colliding remote tag")
	}
	if got := gitAt(t, remote, "rev-parse", "refs/heads/main^{commit}"); got != remoteMainBefore {
		t.Fatalf("remote main changed to %s, want %s", got, remoteMainBefore)
	}
	if got := gitAt(t, remote, "rev-parse", "refs/tags/v1.0.0^{commit}"); got != remoteTagBefore {
		t.Fatalf("remote tag changed to %s, want %s", got, remoteTagBefore)
	}
}

func TestGitRepositoryPushAtomicRejectsOptionShapedOrInvalidRemote(t *testing.T) {
	repo := newTestRepository(t)
	repo.commit(t, "release")
	repo.git(t, "tag", "v1.0.0")
	for _, remote := range []string{"", "--mirror", "origin:other"} {
		t.Run(remote, func(t *testing.T) {
			err := NewGitRepository(repo.dir).PushAtomic(context.Background(), remote, "main", "v1.0.0")
			if err == nil || !strings.Contains(err.Error(), "invalid remote") {
				t.Fatalf("PushAtomic remote %q error = %v, want validation error", remote, err)
			}
		})
	}
}

type testRepository struct{ dir string }

func newTestRepository(t *testing.T) testRepository {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "--initial-branch=main")
	gitAt(t, dir, "config", "user.name", "Test User")
	gitAt(t, dir, "config", "user.email", "test@example.com")
	return testRepository{dir: dir}
}

func newRemoteRepository(t *testing.T) (testRepository, string) {
	t.Helper()
	repo := newTestRepository(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustRun(t, repo.dir, "git", "init", "--bare", "--initial-branch=main", remote)
	repo.git(t, "remote", "add", "origin", remote)
	return repo, remote
}

func (r testRepository) write(t *testing.T, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(r.dir, name), content)
}

func (r testRepository) commit(t *testing.T, message string) {
	t.Helper()
	r.write(t, "tracked.txt", message+"\n")
	r.git(t, "add", "tracked.txt")
	r.git(t, "commit", "-m", message)
}

func (r testRepository) commitMessage(t *testing.T, subject, body string) {
	t.Helper()
	r.write(t, "tracked.txt", subject+"\n")
	r.git(t, "add", "tracked.txt")
	r.git(t, "commit", "--cleanup=verbatim", "-m", subject, "-m", body)
}

func (r testRepository) git(t *testing.T, args ...string) string {
	t.Helper()
	return gitAt(t, r.dir, args...)
}

func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func refExists(t *testing.T, dir, ref string) bool {
	t.Helper()
	command := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	command.Dir = dir
	err := command.Run()
	if err == nil {
		return true
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git show-ref %s: %v", ref, err)
	return false
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
