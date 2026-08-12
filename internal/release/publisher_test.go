package release

import (
	"context"
	"strings"
	"testing"
)

func TestPublisherPublishCommitsOnlyManagedFilesTagsAndPushesAtomically(t *testing.T) {
	repo, remote := newRemoteRepository(t)
	repo.write(t, "cmd/root.go", "package main\n\nconst Version = \"1.0.0\"\n")
	repo.write(t, "tests/cli.bats", "VERSION=1.0.0\n")
	repo.write(t, "CHANGELOG.md", "# Changelog\n\n## v1.0.0\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "initial")
	repo.git(t, "push", "origin", "main")
	repo.write(t, "cmd/root.go", "package main\n\nconst Version = \"1.1.0\"\n")
	repo.write(t, "tests/cli.bats", "VERSION=1.1.0\n")
	repo.write(t, "CHANGELOG.md", "# Changelog\n\n## v1.1.0\n")

	publisher := NewPublisher(NewGitRepository(repo.dir), "origin")
	if err := publisher.Publish(context.Background(), "1.1.0"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got := repo.git(t, "log", "-1", "--format=%s"); got != "chore(release): bump version to 1.1.0" {
		t.Fatalf("release subject = %q", got)
	}
	if got := strings.Fields(repo.git(t, "show", "--format=", "--name-only", "HEAD")); strings.Join(got, ",") != "CHANGELOG.md,cmd/root.go,tests/cli.bats" {
		t.Fatalf("release files = %v", got)
	}
	if got := gitAt(t, remote, "rev-parse", "refs/tags/v1.1.0"); got == "" {
		t.Fatal("release tag was not pushed")
	}
}

func TestPublisherRejectsWrongBranchTagCollisionAndUnrelatedStagedPath(t *testing.T) {
	t.Run("wrong branch", func(t *testing.T) {
		repo, _ := preparedReleaseRepository(t, "1.1.0")
		repo.git(t, "checkout", "-b", "release")
		if err := NewPublisher(NewGitRepository(repo.dir), "origin").Publish(context.Background(), "1.1.0"); err == nil || !strings.Contains(err.Error(), "main") {
			t.Fatalf("Publish wrong branch error = %v", err)
		}
	})
	t.Run("tag collision", func(t *testing.T) {
		repo, _ := preparedReleaseRepository(t, "1.1.0")
		repo.git(t, "tag", "v1.1.0")
		if err := NewPublisher(NewGitRepository(repo.dir), "origin").Publish(context.Background(), "1.1.0"); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("Publish tag collision error = %v", err)
		}
	})
	t.Run("unrelated staged path", func(t *testing.T) {
		repo, _ := preparedReleaseRepository(t, "1.1.0")
		repo.write(t, "unrelated.txt", "do not commit\n")
		repo.git(t, "add", "unrelated.txt")
		if err := NewPublisher(NewGitRepository(repo.dir), "origin").Publish(context.Background(), "1.1.0"); err == nil || !strings.Contains(err.Error(), "unmanaged path") {
			t.Fatalf("Publish unrelated staging error = %v", err)
		}
	})
}

func TestPublisherRejectsInvalidStableVersionBeforeMutation(t *testing.T) {
	for _, version := range []string{"next", "1.2.3-rc.1", "1.2.3/candidate", "v1.2.3", "01.2.3"} {
		t.Run(version, func(t *testing.T) {
			repo, _ := preparedReleaseRepository(t, "1.2.3")
			headBefore := repo.git(t, "rev-parse", "HEAD")
			statusBefore := repo.git(t, "status", "--porcelain=v1")

			err := NewPublisher(NewGitRepository(repo.dir), "origin").Publish(context.Background(), version)
			if err == nil || !strings.Contains(err.Error(), "stable X.Y.Z") {
				t.Fatalf("Publish version %q error = %v, want stable-version validation", version, err)
			}
			if got := repo.git(t, "rev-parse", "HEAD"); got != headBefore {
				t.Fatalf("HEAD changed to %s, want %s", got, headBefore)
			}
			if got := repo.git(t, "status", "--porcelain=v1"); got != statusBefore {
				t.Fatalf("status changed after validation: %q, want %q", got, statusBefore)
			}
		})
	}
}

func TestPublisherRejectsRenameWithAnyUnmanagedSide(t *testing.T) {
	t.Run("unmanaged source", func(t *testing.T) {
		repo, _ := newRemoteRepository(t)
		repo.write(t, "cmd/.keep", "")
		repo.write(t, "legacy-root.go", "package main\n")
		repo.write(t, "tests/cli.bats", "VERSION=1.0.0\n")
		repo.write(t, "CHANGELOG.md", "# Changelog\n")
		repo.git(t, "add", ".")
		repo.git(t, "commit", "-m", "initial")
		repo.git(t, "push", "origin", "main")
		repo.git(t, "mv", "legacy-root.go", "cmd/root.go")
		repo.write(t, "tests/cli.bats", "VERSION=1.1.0\n")
		repo.write(t, "CHANGELOG.md", "# Changelog\n\n## v1.1.0\n")

		err := NewPublisher(NewGitRepository(repo.dir), "origin").Publish(context.Background(), "1.1.0")
		if err == nil || !strings.Contains(err.Error(), "unmanaged path") {
			t.Fatalf("Publish unmanaged rename source error = %v", err)
		}
	})
	t.Run("unmanaged destination", func(t *testing.T) {
		repo, _ := preparedReleaseRepository(t, "1.1.0")
		repo.git(t, "mv", "cmd/root.go", "legacy-root.go")

		err := NewPublisher(NewGitRepository(repo.dir), "origin").Publish(context.Background(), "1.1.0")
		if err == nil || !strings.Contains(err.Error(), "unmanaged path") {
			t.Fatalf("Publish unmanaged rename destination error = %v", err)
		}
	})
}

func preparedReleaseRepository(t *testing.T, version string) (testRepository, string) {
	t.Helper()
	repo, remote := newRemoteRepository(t)
	repo.write(t, "cmd/root.go", "package main\n")
	repo.write(t, "tests/cli.bats", "VERSION=1.0.0\n")
	repo.write(t, "CHANGELOG.md", "# Changelog\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "initial")
	repo.git(t, "push", "origin", "main")
	repo.write(t, "cmd/root.go", "package main\n// "+version+"\n")
	repo.write(t, "tests/cli.bats", "VERSION="+version+"\n")
	repo.write(t, "CHANGELOG.md", "# Changelog\n\n## v"+version+"\n")
	return repo, remote
}
