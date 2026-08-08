package release

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestServicePlanParsesCommitsAndComputesRelease(t *testing.T) {
	repository := &serviceRepository{
		latestTag: "v0.5.0",
		commits: []Commit{
			{Hash: "bbb", Subject: "fix: repair parser"},
			{Hash: "aaa", Subject: "feat(api)!: replace schema", Body: "details"},
			{Hash: "ccc", Subject: "chore: tidy"},
		},
	}
	service := &Service{repository: repository}

	got, err := service.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !got.ReleaseNeeded || got.PreviousVersion != "0.5.0" || got.Version != "1.0.0" || got.Tag != "v1.0.0" || got.Bump != BumpMajor {
		t.Fatalf("Plan metadata = %#v", got)
	}
	if len(got.Commits) != 3 || got.Commits[0].Type != "fix" || got.Commits[1].Scope != "api" || !got.Commits[1].Breaking || got.Commits[2].Type != "chore" {
		t.Fatalf("Plan commits = %#v, want parsed Conventional Commits", got.Commits)
	}
	if !reflect.DeepEqual(repository.calls, []string{"latest-tag", "commits:v0.5.0"}) {
		t.Fatalf("repository calls = %v", repository.calls)
	}
}

func TestServicePlanNoReleaseKeepsCurrentVersionAndNonNilCommits(t *testing.T) {
	service := &Service{repository: &serviceRepository{
		latestTag: "v2.3.4",
		commits:   []Commit{{Hash: "abc", Subject: "ci: refresh runner"}},
	}}

	got, err := service.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got.ReleaseNeeded || got.PreviousVersion != "2.3.4" || got.Version != "2.3.4" || got.Tag != "v2.3.4" || got.Bump != BumpNone {
		t.Fatalf("no-release Plan = %#v", got)
	}
	if got.Commits == nil || len(got.Commits) != 1 || got.Commits[0].Type != "ci" {
		t.Fatalf("no-release commits = %#v", got.Commits)
	}
}

func TestServicePrepareReleaseUpdatesOnlyManagedFilesThenAppendsOutputs(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n\n## v0.5.0\n\nOld notes.\n")
	writeFile(t, filepath.Join(root, "unrelated.txt"), "untouched\n")
	output := filepath.Join(root, "github-output.txt")
	writeFile(t, output, "existing=yes\n")
	repository := &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc123", Subject: "feat(cli): add release command"}},
	}
	service := &Service{directory: root, repository: repository}

	plan, err := service.Prepare(context.Background(), output)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Version != "0.6.0" || plan.Bump != BumpMinor {
		t.Fatalf("Prepare plan = %#v", plan)
	}
	assertFileContains(t, filepath.Join(root, "cmd", "root.go"), `Version: "0.6.0"`)
	assertFileContains(t, filepath.Join(root, "tests", "cli.bats"), `emberfall version 0.6.0`)
	changelog := readServiceFile(t, filepath.Join(root, "CHANGELOG.md"))
	wantSection := "## v0.6.0\n\n### Features\n- add release command ([abc123](https://github.com/aquia-inc/emberfall/commit/abc123))\n"
	if !strings.HasPrefix(changelog, "# Changelog\n\n"+wantSection+"\n## v0.5.0") {
		t.Fatalf("CHANGELOG.md = %q, want canonical section prepended", changelog)
	}
	if got := readServiceFile(t, filepath.Join(root, "unrelated.txt")); got != "untouched\n" {
		t.Fatalf("unrelated file changed to %q", got)
	}
	wantOutput := "existing=yes\n" + githubOutput(releaseOutputValues{
		releaseNeeded:   true,
		previousVersion: "0.5.0",
		version:         "0.6.0",
		tag:             "v0.6.0",
		bump:            BumpMinor,
	})
	if got := readServiceFile(t, output); got != wantOutput {
		t.Fatalf("GitHub output = %q, want %q", got, wantOutput)
	}
	if len(repository.calls) == 0 || repository.calls[0] != "ensure-clean" {
		t.Fatalf("Prepare calls = %v, want cleanliness check first", repository.calls)
	}
}

func TestServicePrepareNoReleaseWritesOutputsWithoutChangingManagedFiles(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
	batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))
	changelogBefore := readServiceFile(t, filepath.Join(root, "CHANGELOG.md"))
	output := filepath.Join(root, "github-output.txt")
	service := &Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "chore: no release"}},
	}}

	plan, err := service.Prepare(context.Background(), output)
	if err != nil {
		t.Fatalf("Prepare no release: %v", err)
	}
	if plan.ReleaseNeeded {
		t.Fatalf("Prepare no-release plan = %#v", plan)
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatalf("root.go changed in no-release preparation")
	}
	if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
		t.Fatalf("cli.bats changed in no-release preparation")
	}
	if got := readServiceFile(t, filepath.Join(root, "CHANGELOG.md")); got != changelogBefore {
		t.Fatalf("CHANGELOG.md changed in no-release preparation")
	}
	want := "release_needed=false\nprevious_version=0.5.0\nversion=0.5.0\ntag=v0.5.0\nbump=none\n"
	if got := readServiceFile(t, output); got != want {
		t.Fatalf("no-release GitHub output = %q, want %q", got, want)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("stat new GitHub output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("new GitHub output mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestServicePrepareNoReleaseRejectsStaleVersionLiteralsBeforeWritingOutput(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.4.0")
	changelogPath := filepath.Join(root, "CHANGELOG.md")
	writeFile(t, changelogPath, "# Changelog\n\n## v0.4.0\n\nOld notes.\n")
	output := filepath.Join(root, "github-output.txt")
	writeFile(t, output, "existing=yes\n")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
	batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))
	changelogBefore := readServiceFile(t, changelogPath)
	service := &Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "chore: no release"}},
	}}

	_, err := service.Prepare(context.Background(), output)
	if err == nil || !strings.Contains(err.Error(), "previous version") {
		t.Fatalf("Prepare no-release stale versions error = %v, want baseline rejection", err)
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatalf("no-release stale preparation changed root.go to %q", got)
	}
	if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
		t.Fatalf("no-release stale preparation changed cli.bats to %q", got)
	}
	if got := readServiceFile(t, changelogPath); got != changelogBefore {
		t.Fatalf("no-release stale preparation changed CHANGELOG.md to %q", got)
	}
	if got := readServiceFile(t, output); got != "existing=yes\n" {
		t.Fatalf("no-release stale preparation changed GitHub output to %q", got)
	}
}

func TestServicePreparePreservesExistingGitHubOutputContentAndMode(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	output := filepath.Join(t.TempDir(), "github-output.txt")
	writeFile(t, output, "existing=yes\n")
	if err := os.Chmod(output, 0o640); err != nil {
		t.Fatal(err)
	}
	service := &Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "chore: no release"}},
	}}

	if _, err := service.Prepare(context.Background(), output); err != nil {
		t.Fatalf("Prepare existing output: %v", err)
	}
	want := "existing=yes\nrelease_needed=false\nprevious_version=0.5.0\nversion=0.5.0\ntag=v0.5.0\nbump=none\n"
	if got := readServiceFile(t, output); got != want {
		t.Fatalf("existing GitHub output = %q, want %q", got, want)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("existing GitHub output mode = %04o, want 0640", info.Mode().Perm())
	}
}

func TestServicePrepareRejectsUnsafeGitHubOutputTargetsBeforeManagedMutation(t *testing.T) {
	tests := []struct {
		name   string
		output func(*testing.T, string) string
	}{
		{name: "managed path", output: func(_ *testing.T, root string) string {
			return filepath.Join(root, "cmd", "root.go")
		}},
		{name: "symlink", output: func(t *testing.T, root string) string {
			target := filepath.Join(t.TempDir(), "target.txt")
			writeFile(t, target, "safe\n")
			path := filepath.Join(root, "output-link")
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "parent symlink to managed path", output: func(t *testing.T, root string) string {
			alias := filepath.Join(t.TempDir(), "cmd-alias")
			if err := os.Symlink(filepath.Join(root, "cmd"), alias); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(alias, "root.go")
		}},
		{name: "hard link to managed path", output: func(t *testing.T, root string) string {
			alias := filepath.Join(t.TempDir(), "root-hard-link.go")
			if err := os.Link(filepath.Join(root, "cmd", "root.go"), alias); err != nil {
				t.Fatal(err)
			}
			return alias
		}},
		{name: "directory", output: func(t *testing.T, _ string) string {
			return t.TempDir()
		}},
		{name: "fifo", output: func(t *testing.T, root string) string {
			path := filepath.Join(root, "output.fifo")
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeServiceVersionTargets(t, root, "0.5.0")
			rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
			batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))
			output := test.output(t, root)
			repository := &serviceRepository{
				latestTag: "v0.5.0",
				commits:   []Commit{{Hash: "abc", Subject: "feat: release"}},
			}

			_, err := (&Service{directory: root, repository: repository}).Prepare(context.Background(), output)
			if err == nil || !strings.Contains(err.Error(), "GitHub output") {
				t.Fatalf("Prepare unsafe output error = %v", err)
			}
			if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
				t.Fatalf("unsafe output changed root.go to %q", got)
			}
			if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
				t.Fatalf("unsafe output changed cli.bats to %q", got)
			}
			if _, statErr := os.Stat(filepath.Join(root, "CHANGELOG.md")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unsafe output CHANGELOG stat error = %v, want not exist", statErr)
			}
		})
	}
}

func TestServicePrepareUsesResolvedOutputPathAfterParentSymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
	batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))

	safeDirectory := t.TempDir()
	resolvedSafeDirectory, err := filepath.EvalSymlinks(safeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	safeOutput := filepath.Join(safeDirectory, "root.go")
	writeFile(t, safeOutput, "existing=yes\n")
	if err := os.Chmod(safeOutput, 0o640); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "output-parent")
	if err := os.Symlink(safeDirectory, alias); err != nil {
		t.Fatal(err)
	}
	unresolvedOutput := filepath.Join(alias, "root.go")

	operations := operatingServiceFiles
	var createDirectory string
	operations.createTemp = func(directory, pattern string) (*os.File, error) {
		createDirectory = directory
		if err := os.Remove(alias); err != nil {
			return nil, err
		}
		if err := os.Symlink(filepath.Join(root, "cmd"), alias); err != nil {
			return nil, err
		}
		return os.CreateTemp(directory, pattern)
	}
	service := &Service{
		directory: root,
		repository: &serviceRepository{
			latestTag: "v0.5.0",
			commits:   []Commit{{Hash: "abc", Subject: "chore: no release"}},
		},
		files: operations,
	}

	if _, err := service.Prepare(context.Background(), unresolvedOutput); err != nil {
		t.Fatalf("Prepare retargeted output parent: %v", err)
	}
	if createDirectory != resolvedSafeDirectory {
		t.Fatalf("output temporary directory = %q, want resolved %q", createDirectory, resolvedSafeDirectory)
	}
	wantOutput := "existing=yes\nrelease_needed=false\nprevious_version=0.5.0\nversion=0.5.0\ntag=v0.5.0\nbump=none\n"
	if got := readServiceFile(t, safeOutput); got != wantOutput {
		t.Fatalf("resolved output = %q, want %q", got, wantOutput)
	}
	info, err := os.Stat(safeOutput)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("resolved output mode = %04o, want 0640", info.Mode().Perm())
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatalf("retargeted parent changed root.go to %q", got)
	}
	if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
		t.Fatalf("retargeted parent changed cli.bats to %q", got)
	}
}

func TestServicePrepareRejectsDirtyRepositoryBeforeMutationOrOutput(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	repository := &serviceRepository{
		ensureCleanErr: errors.New("repository has uncommitted changes"),
		latestTag:      "v0.5.0",
		commits:        []Commit{{Hash: "abc", Subject: "feat: release"}},
	}
	output := filepath.Join(root, "github-output.txt")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))

	_, err := (&Service{directory: root, repository: repository}).Prepare(context.Background(), output)
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("Prepare dirty error = %v", err)
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatal("dirty preparation changed root.go")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dirty preparation output stat error = %v, want not exist", err)
	}
	if !reflect.DeepEqual(repository.calls, []string{"ensure-clean"}) {
		t.Fatalf("dirty preparation calls = %v", repository.calls)
	}
}

func TestServicePrepareWritesOutputsOnlyAfterSuccessfulFilePreparation(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	writeFile(t, filepath.Join(root, "tests", "cli.bats"), "no version literal\n")
	output := filepath.Join(root, "github-output.txt")
	service := &Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "fix: release"}},
	}}

	_, err := service.Prepare(context.Background(), output)
	if err == nil || !strings.Contains(err.Error(), "version literal") {
		t.Fatalf("Prepare malformed target error = %v", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preparation output stat error = %v, want not exist", err)
	}
}

func TestServicePrepareRejectsSynchronizedStaleVersionLiteralsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.4.0")
	changelogPath := filepath.Join(root, "CHANGELOG.md")
	writeFile(t, changelogPath, "# Changelog\n\n## v0.4.0\n\nOld notes.\n")
	output := filepath.Join(root, "github-output.txt")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
	batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))
	changelogBefore := readServiceFile(t, changelogPath)
	service := &Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "fix: release"}},
	}}

	_, err := service.Prepare(context.Background(), output)
	if err == nil || !strings.Contains(err.Error(), "previous version") {
		t.Fatalf("Prepare stale versions error = %v, want baseline rejection", err)
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatalf("stale preparation changed root.go to %q", got)
	}
	if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
		t.Fatalf("stale preparation changed cli.bats to %q", got)
	}
	if got := readServiceFile(t, changelogPath); got != changelogBefore {
		t.Fatalf("stale preparation changed CHANGELOG.md to %q", got)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale preparation output stat error = %v, want not exist", statErr)
	}
}

func TestServicePrepareRejectsNoncanonicalVersionLiteralsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "v0.5.0")
	changelogPath := filepath.Join(root, "CHANGELOG.md")
	writeFile(t, changelogPath, "# Changelog\n")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
	batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))
	changelogBefore := readServiceFile(t, changelogPath)
	output := filepath.Join(root, "github-output.txt")

	_, err := (&Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "fix: release"}},
	}}).Prepare(context.Background(), output)
	if err == nil || !strings.Contains(err.Error(), "previous version") {
		t.Fatalf("Prepare noncanonical versions error = %v, want baseline rejection", err)
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatalf("noncanonical preparation changed root.go to %q", got)
	}
	if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
		t.Fatalf("noncanonical preparation changed cli.bats to %q", got)
	}
	if got := readServiceFile(t, changelogPath); got != changelogBefore {
		t.Fatalf("noncanonical preparation changed CHANGELOG.md to %q", got)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("noncanonical preparation output stat error = %v, want not exist", statErr)
	}
}

func TestServicePrepareRestoresReleaseFilesWhenOutputAppendFails(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n\n## v0.5.0\n\nOld notes.\n")
	rootBefore := readServiceFile(t, filepath.Join(root, "cmd", "root.go"))
	batsBefore := readServiceFile(t, filepath.Join(root, "tests", "cli.bats"))
	changelogBefore := readServiceFile(t, filepath.Join(root, "CHANGELOG.md"))
	output := filepath.Join(t.TempDir(), "github-output.txt")
	writeFile(t, output, "original\n")
	operations := operatingServiceFiles
	operations.write = func(file *os.File, contents []byte) (int, error) {
		if strings.Contains(filepath.Base(file.Name()), ".emberfall-output-") {
			return 4, errors.New("injected output write failure")
		}
		return file.Write(contents)
	}
	service := &Service{directory: root, repository: &serviceRepository{
		latestTag: "v0.5.0",
		commits:   []Commit{{Hash: "abc", Subject: "fix: release"}},
	}, files: operations}

	_, err := service.Prepare(context.Background(), output)
	if err == nil || !strings.Contains(err.Error(), "release files were prepared") {
		t.Fatalf("Prepare output error = %v, want release-file rollback error", err)
	}
	if got := readServiceFile(t, filepath.Join(root, "cmd", "root.go")); got != rootBefore {
		t.Fatalf("output failure changed root.go to %q", got)
	}
	if got := readServiceFile(t, filepath.Join(root, "tests", "cli.bats")); got != batsBefore {
		t.Fatalf("output failure changed cli.bats to %q", got)
	}
	if got := readServiceFile(t, filepath.Join(root, "CHANGELOG.md")); got != changelogBefore {
		t.Fatalf("output failure changed CHANGELOG.md to %q", got)
	}
	if got := readServiceFile(t, output); got != "original\n" {
		t.Fatalf("failed output append left partial bytes: %q", got)
	}
}

func TestServicePrepareGitHubOutputFailuresLeaveOriginalBytes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*serviceFileOperations)
		want   []string
	}{
		{
			name: "write",
			mutate: func(operations *serviceFileOperations) {
				operations.write = func(file *os.File, contents []byte) (int, error) {
					if strings.Contains(filepath.Base(file.Name()), ".emberfall-output-") {
						return 3, errors.New("injected write failure")
					}
					return file.Write(contents)
				}
			},
			want: []string{"write replacement", "injected write failure"},
		},
		{
			name: "rename",
			mutate: func(operations *serviceFileOperations) {
				operations.rename = func(source, destination string) error {
					if strings.Contains(filepath.Base(source), ".emberfall-output-") {
						return errors.New("injected rename failure")
					}
					return os.Rename(source, destination)
				}
			},
			want: []string{"replace GitHub output", "injected rename failure"},
		},
		{
			name: "write and cleanup",
			mutate: func(operations *serviceFileOperations) {
				operations.write = func(file *os.File, contents []byte) (int, error) {
					if strings.Contains(filepath.Base(file.Name()), ".emberfall-output-") {
						return 0, errors.New("injected write failure")
					}
					return file.Write(contents)
				}
				operations.remove = func(path string) error {
					if strings.Contains(filepath.Base(path), ".emberfall-output-") {
						return errors.New("injected cleanup failure")
					}
					return os.Remove(path)
				}
			},
			want: []string{"injected write failure", "injected cleanup failure"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeServiceVersionTargets(t, root, "0.5.0")
			output := filepath.Join(t.TempDir(), "github-output.txt")
			writeFile(t, output, "original\n")
			operations := operatingServiceFiles
			test.mutate(&operations)
			service := &Service{
				directory:  root,
				repository: &serviceRepository{latestTag: "v0.5.0", commits: []Commit{{Hash: "abc", Subject: "chore: no release"}}},
				files:      operations,
			}

			_, err := service.Prepare(context.Background(), output)
			if err == nil {
				t.Fatal("Prepare output failure succeeded")
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Prepare output error = %v, want %q", err, want)
				}
			}
			if got := readServiceFile(t, output); got != "original\n" {
				t.Fatalf("failed output replacement changed original to %q", got)
			}
		})
	}
}

func TestServicePrepareJoinsChangelogCleanupFailureAfterVersionApplyFailure(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	versionOperations := operatingSystemVersionFiles
	originalVersionOperations := operatingSystemVersionFiles
	t.Cleanup(func() { operatingSystemVersionFiles = originalVersionOperations })
	versionOperations.rename = func(source, destination string) error {
		if destination == filepath.Join(root, "cmd", "root.go") {
			return errors.New("injected version apply failure")
		}
		return os.Rename(source, destination)
	}
	operatingSystemVersionFiles = versionOperations
	operations := operatingServiceFiles
	operations.remove = func(path string) error {
		if strings.Contains(filepath.Base(path), ".emberfall-changelog-") {
			return errors.New("injected changelog cleanup failure")
		}
		return os.Remove(path)
	}
	service := &Service{
		directory:  root,
		repository: &serviceRepository{latestTag: "v0.5.0", commits: []Commit{{Hash: "abc", Subject: "fix: release"}}},
		files:      operations,
	}

	_, err := service.Prepare(context.Background(), filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "injected version apply failure") || !strings.Contains(err.Error(), "injected changelog cleanup failure") {
		t.Fatalf("Prepare version/cleanup error = %v", err)
	}
}

func TestServicePrepareJoinsChangelogCleanupFailureAfterRenameFailure(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.5.0")
	operations := operatingServiceFiles
	operations.rename = func(source, destination string) error {
		if destination == filepath.Join(root, "CHANGELOG.md") {
			return errors.New("injected changelog rename failure")
		}
		return os.Rename(source, destination)
	}
	operations.remove = func(path string) error {
		if strings.Contains(filepath.Base(path), ".emberfall-changelog-") {
			return errors.New("injected changelog cleanup failure")
		}
		return os.Remove(path)
	}
	service := &Service{
		directory:  root,
		repository: &serviceRepository{latestTag: "v0.5.0", commits: []Commit{{Hash: "abc", Subject: "fix: release"}}},
		files:      operations,
	}

	_, err := service.Prepare(context.Background(), filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "injected changelog rename failure") || !strings.Contains(err.Error(), "injected changelog cleanup failure") {
		t.Fatalf("Prepare rename/cleanup error = %v", err)
	}
	assertFileContains(t, filepath.Join(root, "cmd", "root.go"), `Version: "0.5.0"`)
	if _, statErr := os.Stat(filepath.Join(root, "CHANGELOG.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed changelog rename created target: %v", statErr)
	}
}

func TestServicePrepareRestoresExactVersionSourcesWhenChangelogReplacementFails(t *testing.T) {
	root := t.TempDir()
	rootPath := filepath.Join(root, "cmd", "root.go")
	batsPath := filepath.Join(root, "tests", "cli.bats")
	changelogPath := filepath.Join(root, "CHANGELOG.md")
	rootOriginal := "// retained header\r\npackage cmd\r\nVersion: \"0.5.0\"\r\n// retained trailer\r\n"
	batsOriginal := "# retained header\r\nassert_output \"emberfall version 0.5.0\"\r\n# retained trailer\r\n"
	changelogOriginal := "# Changelog\r\n\r\n## v0.5.0\r\n\r\nOld notes.\r\n"
	writeFile(t, rootPath, rootOriginal)
	writeFile(t, batsPath, batsOriginal)
	writeFile(t, changelogPath, changelogOriginal)
	for path, mode := range map[string]os.FileMode{rootPath: 0o640, batsPath: 0o600, changelogPath: 0o644} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("chmod %s: %v", path, err)
		}
	}
	operations := operatingServiceFiles
	operations.rename = func(source, destination string) error {
		if destination == changelogPath {
			return errors.New("injected changelog rename failure")
		}
		return os.Rename(source, destination)
	}
	service := &Service{
		directory:  root,
		repository: &serviceRepository{latestTag: "v0.5.0", commits: []Commit{{Hash: "abc", Subject: "fix: release"}}},
		files:      operations,
	}

	_, err := service.Prepare(context.Background(), filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "injected changelog rename failure") {
		t.Fatalf("Prepare changelog replacement failure = %v", err)
	}
	for path, want := range map[string]string{rootPath: rootOriginal, batsPath: batsOriginal, changelogPath: changelogOriginal} {
		if got := readServiceFile(t, path); got != want {
			t.Errorf("%s after failed preparation = %q, want exact original %q", path, got, want)
		}
	}
	for path, wantMode := range map[string]os.FileMode{rootPath: 0o640, batsPath: 0o600, changelogPath: 0o644} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", path, statErr)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Errorf("%s mode after failed preparation = %#o, want %#o", path, got, wantMode)
		}
	}
}

func TestServicePublishValidatesExactCanonicalPreparedStateBeforeDelegating(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.6.0")
	commits := []Commit{{Hash: "abc", Subject: "feat: release"}}
	section := RenderChangelog(Version{Major: 0, Minor: 6}, commits)
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n\n"+section)
	publisher := &servicePublisher{}
	service := &Service{
		directory:  root,
		repository: &serviceRepository{latestTag: "v0.5.0", commits: commits},
		publisher:  publisher,
	}

	if err := service.Publish(context.Background(), "0.6.0"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publisher.version != "0.6.0" || publisher.calls != 1 {
		t.Fatalf("publisher calls = %d, version = %q", publisher.calls, publisher.version)
	}

	writeServiceVersionTargets(t, root, "0.7.0")
	if err := service.Publish(context.Background(), "0.6.0"); err == nil || !strings.Contains(err.Error(), "cmd/root.go") {
		t.Fatalf("Publish mismatched prepared version error = %v", err)
	}
	if publisher.calls != 1 {
		t.Fatalf("publisher called after validation failure: %d", publisher.calls)
	}
}

func TestServicePublishRejectsNonCanonicalVersionAndChangelog(t *testing.T) {
	root := t.TempDir()
	writeServiceVersionTargets(t, root, "0.6.0")
	commits := []Commit{{Hash: "abc", Subject: "feat: release"}}
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n\n## v0.6.0\n\nHand edited.\n")
	publisher := &servicePublisher{}
	service := &Service{directory: root, repository: &serviceRepository{latestTag: "v0.5.0", commits: commits}, publisher: publisher}

	if err := service.Publish(context.Background(), "v0.6.0"); err == nil || !strings.Contains(err.Error(), "X.Y.Z") {
		t.Fatalf("Publish v-prefixed version error = %v", err)
	}
	if err := service.Publish(context.Background(), "0.6.0"); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("Publish noncanonical changelog error = %v", err)
	}
	if publisher.calls != 0 {
		t.Fatalf("publisher called %d times on invalid prepared state", publisher.calls)
	}
}

func TestServicePreparePublishRetriesFailedAtomicPushAgainstRealRepository(t *testing.T) {
	repo, remote := newRemoteRepository(t)
	writeServiceVersionTargets(t, repo.dir, "0.5.0")
	repo.write(t, "CHANGELOG.md", "# Changelog\n\n## v0.5.0\n\nInitial release.\n")
	repo.write(t, "application.txt", "initial\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "chore: baseline")
	repo.git(t, "tag", "v0.5.0")
	repo.git(t, "push", "origin", "main", "refs/tags/v0.5.0")
	repo.write(t, "application.txt", "feature\n")
	repo.git(t, "add", "application.txt")
	repo.git(t, "commit", "-m", "feat(cli): add release administration")
	repo.git(t, "push", "origin", "main")
	preReleaseHead := repo.git(t, "rev-parse", "HEAD")

	hook := filepath.Join(remote, "hooks", "pre-receive")
	writeFile(t, hook, "#!/bin/sh\necho injected atomic rejection >&2\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatalf("chmod pre-receive hook: %v", err)
	}
	service := NewService(repo.dir, "origin")
	output := filepath.Join(t.TempDir(), "github-output.txt")
	plan, err := service.Prepare(context.Background(), output)
	if err != nil {
		t.Fatalf("Prepare real repository: %v", err)
	}
	if !plan.ReleaseNeeded || plan.PreviousVersion != "0.5.0" || plan.Version != "0.6.0" || plan.Tag != "v0.6.0" || plan.Bump != BumpMinor {
		t.Fatalf("real Prepare plan = %#v", plan)
	}
	wantPreparedPaths := "CHANGELOG.md,cmd/root.go,tests/cli.bats"
	if got := strings.Join(strings.Fields(repo.git(t, "diff", "--name-only")), ","); got != wantPreparedPaths {
		t.Fatalf("prepared paths = %q, want %q", got, wantPreparedPaths)
	}

	if err := service.Publish(context.Background(), "0.6.0"); err == nil || !strings.Contains(err.Error(), "injected atomic rejection") {
		t.Fatalf("Publish rejected atomic push error = %v", err)
	}
	if got := repo.git(t, "log", "-1", "--format=%s"); got != "chore(release): bump version to 0.6.0" {
		t.Fatalf("local release subject = %q", got)
	}
	if got := strings.Join(strings.Fields(repo.git(t, "show", "--format=", "--name-only", "HEAD")), ","); got != wantPreparedPaths {
		t.Fatalf("release commit paths = %q, want %q", got, wantPreparedPaths)
	}
	if got := gitAt(t, remote, "rev-parse", "refs/heads/main^{commit}"); got != preReleaseHead {
		t.Fatalf("failed atomic push changed remote main to %s, want %s", got, preReleaseHead)
	}
	if refExists(t, remote, "refs/tags/v0.6.0") {
		t.Fatal("failed atomic push created remote release tag")
	}

	if err := os.Remove(hook); err != nil {
		t.Fatalf("remove injected pre-receive hook: %v", err)
	}
	if err := service.Publish(context.Background(), "0.6.0"); err != nil {
		t.Fatalf("Publish retry: %v", err)
	}
	localRelease := repo.git(t, "rev-parse", "HEAD^{commit}")
	remoteMain := gitAt(t, remote, "rev-parse", "refs/heads/main^{commit}")
	remoteTag := gitAt(t, remote, "rev-parse", "refs/tags/v0.6.0^{commit}")
	if remoteMain != localRelease || remoteTag != localRelease {
		t.Fatalf("remote main = %s, tag = %s, local = %s", remoteMain, remoteTag, localRelease)
	}
	if err := service.Publish(context.Background(), "0.6.0"); err != nil {
		t.Fatalf("already-completed Publish retry: %v", err)
	}
	noRelease, err := service.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan after release: %v", err)
	}
	if noRelease.ReleaseNeeded || noRelease.PreviousVersion != "0.6.0" || noRelease.Version != "0.6.0" || noRelease.Tag != "v0.6.0" || noRelease.Bump != BumpNone {
		t.Fatalf("post-release Plan = %#v, want no release", noRelease)
	}
}

func TestServicePublishRetryRejectsUnvalidatedLocalReleaseCandidates(t *testing.T) {
	t.Run("wrong subject", func(t *testing.T) {
		_, service := newLocalRetryCandidate(t, "chore: not a generated release", false, true)
		err := service.Publish(context.Background(), "0.6.0")
		if err == nil || !strings.Contains(err.Error(), "generated release subject") {
			t.Fatalf("Publish retry wrong-subject error = %v", err)
		}
	})

	t.Run("extra committed path", func(t *testing.T) {
		_, service := newLocalRetryCandidate(t, "chore(release): bump version to 0.6.0", true, true)
		err := service.Publish(context.Background(), "0.6.0")
		if err == nil || !strings.Contains(err.Error(), "exactly the managed release paths") {
			t.Fatalf("Publish retry extra-path error = %v", err)
		}
	})

	t.Run("dirty worktree", func(t *testing.T) {
		repo, service := newLocalRetryCandidate(t, "chore(release): bump version to 0.6.0", false, true)
		repo.write(t, "dirty.txt", "dirty\n")
		err := service.Publish(context.Background(), "0.6.0")
		if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
			t.Fatalf("Publish retry dirty error = %v", err)
		}
	})

	t.Run("noncanonical changelog", func(t *testing.T) {
		_, service := newLocalRetryCandidate(t, "chore(release): bump version to 0.6.0", false, false)
		err := service.Publish(context.Background(), "0.6.0")
		if err == nil || !strings.Contains(err.Error(), "canonical") {
			t.Fatalf("Publish retry changelog error = %v", err)
		}
	})
}

func newLocalRetryCandidate(t *testing.T, subject string, extraPath, canonicalChangelog bool) (testRepository, *Service) {
	t.Helper()
	repo, _ := newRemoteRepository(t)
	writeServiceVersionTargets(t, repo.dir, "0.5.0")
	repo.write(t, "CHANGELOG.md", "# Changelog\n\n## v0.5.0\n\nInitial release.\n")
	repo.write(t, "application.txt", "initial\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "chore: baseline")
	repo.git(t, "tag", "v0.5.0")
	repo.git(t, "push", "origin", "main", "refs/tags/v0.5.0")
	repo.write(t, "application.txt", "feature\n")
	repo.git(t, "add", "application.txt")
	repo.git(t, "commit", "-m", "feat: change behavior")
	repo.git(t, "push", "origin", "main")
	featureHash := repo.git(t, "rev-parse", "HEAD")
	if err := UpdateVersionFiles(repo.dir, Version{Major: 0, Minor: 6}); err != nil {
		t.Fatalf("prepare retry-candidate versions: %v", err)
	}
	section := "## v0.6.0\n\nHand edited.\n"
	if canonicalChangelog {
		section = RenderChangelog(Version{Major: 0, Minor: 6}, []Commit{{Hash: featureHash, Subject: "feat: change behavior"}})
	}
	repo.write(t, "CHANGELOG.md", "# Changelog\n\n"+section+"\n## v0.5.0\n\nInitial release.\n")
	paths := append([]string(nil), releasePaths...)
	if extraPath {
		repo.write(t, "extra.txt", "extra\n")
		paths = append(paths, "extra.txt")
	}
	repo.git(t, append([]string{"add", "--"}, paths...)...)
	repo.git(t, "commit", "-m", subject)
	repo.git(t, "tag", "v0.6.0")
	return repo, NewService(repo.dir, "origin")
}

func TestServiceNotesReturnsExactlyExtractedSection(t *testing.T) {
	root := t.TempDir()
	want := "## v0.6.0\n\n### Fixes\n- repaired\n"
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n\n"+want+"\n## v0.5.0\n\nOld\n")

	got, err := (&Service{directory: root}).Notes(context.Background(), "v0.6.0")
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if got != want {
		t.Fatalf("Notes = %q, want exact %q", got, want)
	}
}

func TestServiceNotesRejectsNonCanonicalTagsBeforeReadingChangelog(t *testing.T) {
	service := &Service{directory: filepath.Join(t.TempDir(), "missing")}
	for _, tag := range []string{"0.6.0", "v0.6.0-rc.1", "--help", "v01.6.0", "not-a-tag"} {
		t.Run(tag, func(t *testing.T) {
			_, err := service.Notes(context.Background(), tag)
			if err == nil || !strings.Contains(err.Error(), "vX.Y.Z") {
				t.Fatalf("Notes(%q) error = %v, want canonical tag validation", tag, err)
			}
			if strings.Contains(err.Error(), "changelog") {
				t.Fatalf("Notes(%q) read changelog before validation: %v", tag, err)
			}
		})
	}
}

func TestServiceEnhanceNotesBuildsTaggedContextAndUsesUpdater(t *testing.T) {
	repo := newTestRepository(t)
	writeFile(t, filepath.Join(repo.dir, "CHANGELOG.md"), "# Changelog\n\n## v0.5.0\n\nOld\n")
	writeFile(t, filepath.Join(repo.dir, "feature.txt"), "before\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "chore: baseline")
	repo.git(t, "tag", "v0.5.0")
	wantNotes := "## v0.6.0\n\n### Features\n- add context\n"
	writeFile(t, filepath.Join(repo.dir, "CHANGELOG.md"), "# Changelog\n\n"+wantNotes+"\n## v0.5.0\n\nOld\n")
	writeFile(t, filepath.Join(repo.dir, "feature.txt"), "after\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "feat(cli): add context")
	repo.git(t, "tag", "v0.6.0")

	updater := &serviceUpdater{}
	var factoryOptions EnhanceOptions
	service := &Service{
		directory: repo.dir,
		updaterFactory: func(options EnhanceOptions) (releaseUpdater, error) {
			factoryOptions = options
			return updater, nil
		},
	}
	options := EnhanceOptions{
		Tag:              "v0.6.0",
		AnthropicAPIKey:  "anthropic-secret",
		Model:            "model-name",
		GitHubToken:      "github-secret",
		GitHubRepository: "aquia-inc/emberfall",
	}

	if err := service.EnhanceNotes(context.Background(), options); err != nil {
		t.Fatalf("EnhanceNotes: %v", err)
	}
	if factoryOptions != options {
		t.Fatalf("factory options = %#v, want %#v", factoryOptions, options)
	}
	input := updater.input
	if input.Tag != "v0.6.0" || input.PreviousTag != "v0.5.0" || input.Notes != wantNotes {
		t.Fatalf("enhancement input metadata = %#v", input)
	}
	if len(input.Commits) != 1 || input.Commits[0].Type != "feat" || input.Commits[0].Scope != "cli" {
		t.Fatalf("enhancement commits = %#v", input.Commits)
	}
	if !strings.Contains(input.Diff, "-before") || !strings.Contains(input.Diff, "+after") {
		t.Fatalf("enhancement diff = %q, want tagged diff context", input.Diff)
	}
}

func TestNewServiceCreatesIndependentProviderHTTPClients(t *testing.T) {
	service := NewService(t.TempDir(), "origin")
	updater, err := service.updaterFactory(EnhanceOptions{
		AnthropicAPIKey:  "anthropic-test-key",
		GitHubToken:      "github-test-token",
		GitHubRepository: "acme/emberfall",
	})
	if err != nil {
		t.Fatalf("construct updater: %v", err)
	}
	production, ok := updater.(ReleaseNotesUpdater)
	if !ok {
		t.Fatalf("updater type = %T, want ReleaseNotesUpdater", updater)
	}
	githubClient, ok := production.Releases.(*GitHubReleaseClient)
	if !ok {
		t.Fatalf("release client type = %T, want *GitHubReleaseClient", production.Releases)
	}
	anthropicEnhancer, ok := production.Enhancer.(*AnthropicEnhancer)
	if !ok {
		t.Fatalf("enhancer type = %T, want *AnthropicEnhancer", production.Enhancer)
	}
	githubHTTPClient, ok := githubClient.Client.(*http.Client)
	if !ok {
		t.Fatalf("github HTTP client type = %T, want *http.Client", githubClient.Client)
	}
	anthropicHTTPClient, ok := anthropicEnhancer.Client.(*http.Client)
	if !ok {
		t.Fatalf("anthropic HTTP client type = %T, want *http.Client", anthropicEnhancer.Client)
	}
	if githubHTTPClient == anthropicHTTPClient {
		t.Fatal("GitHub and Anthropic adapters share one HTTP client")
	}
}

func TestServiceEnhanceNotesUsesRealSDKAdaptersWithoutMutatingDeterministicNotesOnFailure(t *testing.T) {
	tests := []struct {
		name              string
		anthropicStatus   int
		githubPatchStatus int
		wantErr           bool
	}{
		{name: "success"},
		{name: "anthropic failure", anthropicStatus: http.StatusServiceUnavailable, wantErr: true},
		{name: "github update failure", githubPatchStatus: http.StatusUnprocessableEntity, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, deterministicNotes := newEnhanceNotesRepository(t)
			publishedBody := deterministicNotes
			anthropicRequests := 0
			patches := 0
			var patch map[string]any

			anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				anthropicRequests++
				if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
					t.Errorf("unexpected Anthropic request: %s %s", request.Method, request.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if request.Header.Get("x-api-key") != "anthropic-test-key" || request.Header.Get("anthropic-version") != "2023-06-01" {
					t.Error("Anthropic SDK request did not set the expected authentication headers")
				}
				var message struct {
					Model    string `json:"model"`
					Messages []struct {
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
					t.Fatalf("decode Anthropic SDK message: %v", err)
				}
				if message.Model != "claude-test" || len(message.Messages) != 1 || len(message.Messages[0].Content) != 1 || !strings.Contains(message.Messages[0].Content[0].Text, "https://example.test/feature") {
					t.Error("Anthropic SDK message omitted configured model or deterministic release-note link")
				}
				if test.anthropicStatus != 0 {
					w.WriteHeader(test.anthropicStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{
					"content": []map[string]string{{
						"type": "text",
						"text": "## Curated\n\n- [Feature](https://example.test/feature)",
					}},
				}); err != nil {
					t.Fatalf("encode Anthropic SDK response: %v", err)
				}
			}))
			defer anthropic.Close()

			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch request.Method + " " + request.URL.Path {
				case "GET /repos/acme/emberfall/releases/tags/v0.6.0":
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(map[string]any{"id": 42, "body": publishedBody}); err != nil {
						t.Fatalf("encode GitHub release: %v", err)
					}
				case "PATCH /repos/acme/emberfall/releases/42":
					patches++
					patch = nil
					if err := json.NewDecoder(request.Body).Decode(&patch); err != nil {
						t.Fatalf("decode GitHub release update: %v", err)
					}
					if test.githubPatchStatus != 0 {
						w.WriteHeader(test.githubPatchStatus)
						return
					}
					body, ok := patch["body"].(string)
					if !ok {
						t.Fatal("GitHub update omitted a string body")
					}
					publishedBody = body
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(map[string]any{"id": 42, "body": body}); err != nil {
						t.Fatalf("encode GitHub update response: %v", err)
					}
				default:
					t.Errorf("unexpected GitHub request: %s %s", request.Method, request.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer github.Close()

			service := &Service{
				directory: repository.dir,
				updaterFactory: func(options EnhanceOptions) (releaseUpdater, error) {
					anthropicEnhancer := NewAnthropicEnhancer(anthropic.Client(), options.AnthropicAPIKey, options.Model)
					anthropicEnhancer.Endpoint = anthropic.URL
					anthropicEnhancer.MaxRetries = 0
					githubClient := NewGitHubReleaseClient(github.Client(), github.URL, options.GitHubToken, "acme", "emberfall")
					githubClient.MaxRetries = 0
					return ReleaseNotesUpdater{Releases: githubClient, Enhancer: anthropicEnhancer}, nil
				},
			}
			err := service.EnhanceNotes(context.Background(), EnhanceOptions{
				Tag:              "v0.6.0",
				AnthropicAPIKey:  "anthropic-test-key",
				Model:            "claude-test",
				GitHubToken:      "github-test-token",
				GitHubRepository: "acme/emberfall",
			})
			if test.wantErr {
				if err == nil {
					t.Fatal("EnhanceNotes succeeded despite provider failure")
				}
				if publishedBody != deterministicNotes {
					t.Fatal("provider failure changed deterministic published notes")
				}
				if test.anthropicStatus != 0 && patches != 0 {
					t.Fatalf("Anthropic failure made %d GitHub PATCH requests, want none", patches)
				}
				if test.githubPatchStatus != 0 && patches != 1 {
					t.Fatalf("GitHub update failure made %d PATCH requests, want one", patches)
				}
				return
			}
			if err != nil {
				t.Fatalf("EnhanceNotes: %v", err)
			}
			if len(patch) != 1 {
				t.Fatalf("GitHub patch = %#v, want only body", patch)
			}
			if !strings.Contains(publishedBody, notesEnhancementMarker) {
				t.Fatal("successful enhancement did not publish the idempotence marker")
			}
			if !strings.Contains(publishedBody, "\n\n- [Feature]") {
				t.Fatal("successful enhancement did not publish multiline Markdown")
			}

			if err := service.EnhanceNotes(context.Background(), EnhanceOptions{
				Tag:              "v0.6.0",
				AnthropicAPIKey:  "anthropic-test-key",
				Model:            "claude-test",
				GitHubToken:      "github-test-token",
				GitHubRepository: "acme/emberfall",
			}); err != nil {
				t.Fatalf("idempotent EnhanceNotes: %v", err)
			}
			if anthropicRequests != 1 || patches != 1 {
				t.Fatalf("idempotent rerun made Anthropic/PATCH requests %d/%d, want 1/1", anthropicRequests, patches)
			}
		})
	}
}

func newEnhanceNotesRepository(t *testing.T) (testRepository, string) {
	t.Helper()
	repository := newTestRepository(t)
	repository.write(t, "CHANGELOG.md", "# Changelog\n\n## v0.5.0\n\nOld notes.\n")
	repository.write(t, "feature.txt", "before\n")
	repository.git(t, "add", ".")
	repository.git(t, "commit", "-m", "chore: establish release baseline")
	repository.git(t, "tag", "v0.5.0")

	deterministicNotes := "## v0.6.0\n\n### Features\n- [Feature](https://example.test/feature)\n"
	repository.write(t, "CHANGELOG.md", "# Changelog\n\n"+deterministicNotes+"\n## v0.5.0\n\nOld notes.\n")
	repository.write(t, "feature.txt", "after\n")
	repository.git(t, "add", ".")
	repository.git(t, "commit", "-m", "feat: add feature")
	repository.git(t, "tag", "v0.6.0")
	return repository, deterministicNotes
}

func TestServiceEnhanceNotesRejectsMissingConfigurationBeforeDependencies(t *testing.T) {
	complete := EnhanceOptions{
		Tag:              "v0.6.0",
		AnthropicAPIKey:  "anthropic",
		GitHubToken:      "github",
		GitHubRepository: "aquia-inc/emberfall",
	}
	tests := []struct {
		name   string
		modify func(*EnhanceOptions)
		want   string
	}{
		{name: "tag", modify: func(o *EnhanceOptions) { o.Tag = "" }, want: "--tag"},
		{name: "anthropic key", modify: func(o *EnhanceOptions) { o.AnthropicAPIKey = "" }, want: "ANTHROPIC_API_KEY"},
		{name: "github token", modify: func(o *EnhanceOptions) { o.GitHubToken = "" }, want: "GITHUB_TOKEN"},
		{name: "github repository", modify: func(o *EnhanceOptions) { o.GitHubRepository = "" }, want: "GITHUB_REPOSITORY"},
		{name: "malformed github repository", modify: func(o *EnhanceOptions) { o.GitHubRepository = "owner/repo/extra" }, want: "GITHUB_REPOSITORY"},
		{name: "leading whitespace", modify: func(o *EnhanceOptions) { o.GitHubRepository = " owner/repo" }, want: "GITHUB_REPOSITORY"},
		{name: "trailing whitespace", modify: func(o *EnhanceOptions) { o.GitHubRepository = "owner/repo " }, want: "GITHUB_REPOSITORY"},
		{name: "control character", modify: func(o *EnhanceOptions) { o.GitHubRepository = "owner/repo\nnext" }, want: "GITHUB_REPOSITORY"},
		{name: "invalid owner", modify: func(o *EnhanceOptions) { o.GitHubRepository = "-owner/repo" }, want: "GITHUB_REPOSITORY"},
		{name: "invalid repository", modify: func(o *EnhanceOptions) { o.GitHubRepository = "owner/repo:name" }, want: "GITHUB_REPOSITORY"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := complete
			test.modify(&options)
			factoryCalled := false
			service := &Service{updaterFactory: func(EnhanceOptions) (releaseUpdater, error) {
				factoryCalled = true
				return &serviceUpdater{}, nil
			}}
			err := service.EnhanceNotes(context.Background(), options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("EnhanceNotes error = %v, want %q", err, test.want)
			}
			if factoryCalled {
				t.Fatal("enhancement dependency constructed for invalid configuration")
			}
		})
	}
}

type serviceRepository struct {
	latestTag      string
	commits        []Commit
	ensureCleanErr error
	calls          []string
}

func (repository *serviceRepository) LatestVersionTag(context.Context) (string, error) {
	repository.calls = append(repository.calls, "latest-tag")
	return repository.latestTag, nil
}

func (repository *serviceRepository) CommitsSince(_ context.Context, tag string) ([]Commit, error) {
	repository.calls = append(repository.calls, "commits:"+tag)
	return append([]Commit(nil), repository.commits...), nil
}

func (repository *serviceRepository) EnsureClean(context.Context) error {
	repository.calls = append(repository.calls, "ensure-clean")
	return repository.ensureCleanErr
}

func (*serviceRepository) CurrentBranch(context.Context) (string, error)   { return "main", nil }
func (*serviceRepository) TagExists(context.Context, string) (bool, error) { return false, nil }
func (*serviceRepository) Commit(context.Context, string, ...string) (string, error) {
	return "", errors.New("unexpected Commit call")
}
func (*serviceRepository) Tag(context.Context, string) error {
	return errors.New("unexpected Tag call")
}
func (*serviceRepository) PushAtomic(context.Context, string, string, string) error {
	return errors.New("unexpected PushAtomic call")
}

type servicePublisher struct {
	version string
	calls   int
	err     error
}

func (publisher *servicePublisher) Publish(_ context.Context, version string) error {
	publisher.calls++
	publisher.version = version
	return publisher.err
}

type serviceUpdater struct {
	input EnhancementInput
	err   error
}

func (updater *serviceUpdater) EnhanceRelease(_ context.Context, input EnhancementInput) (string, error) {
	updater.input = input
	return "enhanced", updater.err
}

func writeServiceVersionTargets(t *testing.T, root, version string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "cmd", "root.go"), "package cmd\n\nvar root = struct{ Version string }{Version: \""+version+"\"}\n")
	writeFile(t, filepath.Join(root, "tests", "cli.bats"), "assert_output \"emberfall version "+version+"\"\n")
}

func readServiceFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	if got := readServiceFile(t, path); !strings.Contains(got, want) {
		t.Fatalf("%s = %q, want substring %q", path, got, want)
	}
}
