package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateVersionFilesUpdatesOnlyVersionLiterals(t *testing.T) {
	root := writeVersionFixture(t,
		"package cmd\n\nvar root = Command{\n\tVersion: \"0.5.0\",\n}\n",
		"@test \"version\" {\n  assert_output \"emberfall version 0.5.0\"\n}\n",
	)

	if err := UpdateVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0}); err != nil {
		t.Fatalf("UpdateVersionFiles: %v", err)
	}

	rootGo := readFile(t, filepath.Join(root, "cmd", "root.go"))
	if want := "package cmd\n\nvar root = Command{\n\tVersion: \"0.6.0\",\n}\n"; rootGo != want {
		t.Errorf("cmd/root.go = %q, want %q", rootGo, want)
	}
	bats := readFile(t, filepath.Join(root, "tests", "cli.bats"))
	if want := "@test \"version\" {\n  assert_output \"emberfall version 0.6.0\"\n}\n"; bats != want {
		t.Errorf("tests/cli.bats = %q, want %q", bats, want)
	}
}

func TestUpdateVersionFilesPreservesPermissionsAndNewlines(t *testing.T) {
	root := writeVersionFixture(t,
		"package cmd\r\nVersion: \"0.5.0\"\r\n",
		"assert_output \"emberfall version 0.5.0\"\r\n",
	)
	rootPath := filepath.Join(root, "cmd", "root.go")
	if err := os.Chmod(rootPath, 0o640); err != nil {
		t.Fatalf("chmod root.go: %v", err)
	}

	if err := UpdateVersionFiles(root, Version{Major: 1, Minor: 0, Patch: 0}); err != nil {
		t.Fatalf("UpdateVersionFiles: %v", err)
	}
	if got := readFile(t, rootPath); got != "package cmd\r\nVersion: \"1.0.0\"\r\n" {
		t.Errorf("root.go newlines/content = %q", got)
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		t.Fatalf("stat root.go: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("root.go permissions = %#o, want %#o", got, os.FileMode(0o640))
	}
}

func TestUpdateVersionFilesValidatesBothFilesBeforeWriting(t *testing.T) {
	rootGo := "package cmd\nVersion: \"0.5.0\"\n"
	bats := "assert_output \"emberfall version 0.5.0\"\nassert_output \"emberfall version 0.5.0\"\n"
	root := writeVersionFixture(t, rootGo, bats)

	if err := UpdateVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0}); err == nil {
		t.Fatal("UpdateVersionFiles succeeded with two BATS literals")
	}
	if got := readFile(t, filepath.Join(root, "cmd", "root.go")); got != rootGo {
		t.Errorf("cmd/root.go changed after validation failure: %q", got)
	}
	if got := readFile(t, filepath.Join(root, "tests", "cli.bats")); got != bats {
		t.Errorf("tests/cli.bats changed after validation failure: %q", got)
	}
}

func TestUpdateVersionFilesRejectsMissingOrAmbiguousTargets(t *testing.T) {
	tests := []struct {
		name   string
		rootGo string
		bats   string
	}{
		{name: "missing cobra version", rootGo: "package cmd\n", bats: "assert_output \"emberfall version 0.5.0\"\n"},
		{name: "multiple cobra versions", rootGo: "Version: \"0.5.0\"\nVersion: \"0.5.0\"\n", bats: "assert_output \"emberfall version 0.5.0\"\n"},
		{name: "missing bats version", rootGo: "Version: \"0.5.0\"\n", bats: "assert_output \"different command\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeVersionFixture(t, tt.rootGo, tt.bats)
			if err := UpdateVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0}); err == nil {
				t.Error("UpdateVersionFiles succeeded, want error")
			}
		})
	}
}

func TestPrepareVersionFilesRejectsMalformedOrMismatchedSources(t *testing.T) {
	tests := []struct {
		name   string
		rootGo string
		bats   string
	}{
		{name: "malformed cobra version", rootGo: "Version: \"banana\"\n", bats: "assert_output \"emberfall version 0.5.0\"\n"},
		{name: "malformed bats version", rootGo: "Version: \"0.5.0\"\n", bats: "assert_output \"emberfall version banana\"\n"},
		{name: "mismatched versions", rootGo: "Version: \"0.5.0\"\n", bats: "assert_output \"emberfall version 0.4.0\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeVersionFixture(t, tt.rootGo, tt.bats)
			if _, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0}); err == nil {
				t.Fatal("PrepareVersionFiles succeeded, want validation error")
			}
			if got := readFile(t, filepath.Join(root, "cmd", "root.go")); got != tt.rootGo {
				t.Errorf("cmd/root.go changed during preparation: %q", got)
			}
			if got := readFile(t, filepath.Join(root, "tests", "cli.bats")); got != tt.bats {
				t.Errorf("tests/cli.bats changed during preparation: %q", got)
			}
		})
	}
}

func TestPrepareVersionFilesRejectsSymlinkAndNonRegularTargets(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := writeVersionFixture(t, "Version: \"0.5.0\"\n", "assert_output \"emberfall version 0.5.0\"\n")
		rootPath := filepath.Join(root, "cmd", "root.go")
		targetPath := filepath.Join(root, "real-root.go")
		original := readFile(t, rootPath)
		if err := os.WriteFile(targetPath, []byte(original), 0o644); err != nil {
			t.Fatalf("write symlink target: %v", err)
		}
		if err := os.Remove(rootPath); err != nil {
			t.Fatalf("remove root fixture: %v", err)
		}
		if err := os.Symlink(targetPath, rootPath); err != nil {
			t.Fatalf("symlink root.go: %v", err)
		}

		if _, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0}); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("PrepareVersionFiles symlink error = %v, want regular-file rejection", err)
		}
		if info, err := os.Lstat(rootPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("root.go symlink topology changed: info=%v err=%v", info, err)
		}
		if got := readFile(t, targetPath); got != original {
			t.Errorf("symlink target changed: %q", got)
		}
	})

	t.Run("directory", func(t *testing.T) {
		root := writeVersionFixture(t, "Version: \"0.5.0\"\n", "assert_output \"emberfall version 0.5.0\"\n")
		rootPath := filepath.Join(root, "cmd", "root.go")
		if err := os.Remove(rootPath); err != nil {
			t.Fatalf("remove root fixture: %v", err)
		}
		if err := os.Mkdir(rootPath, 0o755); err != nil {
			t.Fatalf("mkdir root.go: %v", err)
		}
		if _, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0}); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("PrepareVersionFiles directory error = %v, want regular-file rejection", err)
		}
	})
}

func TestVersionFilesApplyRestoresFirstFileAfterSecondReplaceFailure(t *testing.T) {
	rootGo := "Version: \"0.5.0\"\n"
	bats := "assert_output \"emberfall version 0.5.0\"\n"
	root := writeVersionFixture(t, rootGo, bats)
	files, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0})
	if err != nil {
		t.Fatalf("PrepareVersionFiles: %v", err)
	}

	replaceFailure := errors.New("forced second replacement failure")
	realRename := files.ops.rename
	renameCalls := 0
	files.ops.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return replaceFailure
		}
		return realRename(oldPath, newPath)
	}

	err = files.Apply()
	if !errors.Is(err, replaceFailure) {
		t.Fatalf("Apply error = %v, want replacement failure", err)
	}
	if got := readFile(t, filepath.Join(root, "cmd", "root.go")); got != rootGo {
		t.Errorf("cmd/root.go not restored: %q", got)
	}
	if got := readFile(t, filepath.Join(root, "tests", "cli.bats")); got != bats {
		t.Errorf("tests/cli.bats changed: %q", got)
	}
	assertNoVersionTemps(t, root)
}

func TestVersionFilesRestoreRestoresExactOriginalBytesAndModes(t *testing.T) {
	rootGo := "// retained header\r\nVersion: \"v0.5.0\"\r\n// retained trailer\r\n"
	bats := "# retained header\r\nassert_output \"emberfall version v0.5.0\"\r\n# retained trailer\r\n"
	root := writeVersionFixture(t, rootGo, bats)
	rootPath := filepath.Join(root, "cmd", "root.go")
	batsPath := filepath.Join(root, "tests", "cli.bats")
	if err := os.Chmod(rootPath, 0o640); err != nil {
		t.Fatalf("chmod root.go: %v", err)
	}
	if err := os.Chmod(batsPath, 0o600); err != nil {
		t.Fatalf("chmod cli.bats: %v", err)
	}

	files, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0})
	if err != nil {
		t.Fatalf("PrepareVersionFiles: %v", err)
	}
	if err := files.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := files.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readFile(t, rootPath); got != rootGo {
		t.Errorf("root.go after restore = %q, want exact original %q", got, rootGo)
	}
	if got := readFile(t, batsPath); got != bats {
		t.Errorf("cli.bats after restore = %q, want exact original %q", got, bats)
	}
	for path, wantMode := range map[string]os.FileMode{rootPath: 0o640, batsPath: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Errorf("%s mode = %#o, want %#o", path, got, wantMode)
		}
	}
}

func TestVersionFilesApplyReportsPartialStateWhenRollbackFails(t *testing.T) {
	rootGo := "Version: \"0.5.0\"\n"
	bats := "assert_output \"emberfall version 0.5.0\"\n"
	root := writeVersionFixture(t, rootGo, bats)
	files, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0})
	if err != nil {
		t.Fatalf("PrepareVersionFiles: %v", err)
	}

	replaceFailure := errors.New("forced second replacement failure")
	rollbackFailure := errors.New("forced rollback failure")
	realRename := files.ops.rename
	renameCalls := 0
	files.ops.rename = func(oldPath, newPath string) error {
		renameCalls++
		switch renameCalls {
		case 2:
			return replaceFailure
		case 3:
			return rollbackFailure
		default:
			return realRename(oldPath, newPath)
		}
	}

	err = files.Apply()
	if !errors.Is(err, replaceFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("Apply error = %v, want joined replacement and rollback failures", err)
	}
	if !strings.Contains(err.Error(), "partial version-file state") {
		t.Errorf("Apply error does not identify partial state: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "cmd", "root.go")); got != "Version: \"0.6.0\"\n" {
		t.Errorf("cmd/root.go partial state = %q, want explicit updated state", got)
	}
	if got := readFile(t, filepath.Join(root, "tests", "cli.bats")); got != bats {
		t.Errorf("tests/cli.bats changed: %q", got)
	}
	assertNoVersionTemps(t, root)
}

func TestVersionFilesApplyReportsFailedStagingCleanup(t *testing.T) {
	rootGo := "Version: \"0.5.0\"\n"
	bats := "assert_output \"emberfall version 0.5.0\"\n"
	root := writeVersionFixture(t, rootGo, bats)
	files, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0})
	if err != nil {
		t.Fatalf("PrepareVersionFiles: %v", err)
	}

	writeFailure := errors.New("forced staging write failure")
	cleanupFailure := errors.New("forced staging cleanup failure")
	realRemove := files.ops.remove
	files.ops.write = func(*os.File, []byte) (int, error) {
		return 0, writeFailure
	}
	failedCleanupPath := ""
	files.ops.remove = func(path string) error {
		failedCleanupPath = path
		return cleanupFailure
	}

	err = files.Apply()
	if !errors.Is(err, writeFailure) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("Apply error = %v, want joined staging-write and cleanup failures", err)
	}
	if failedCleanupPath == "" || !strings.Contains(err.Error(), failedCleanupPath) {
		t.Errorf("Apply error does not report leaked temporary path %q: %v", failedCleanupPath, err)
	}
	if _, statErr := os.Lstat(failedCleanupPath); statErr != nil {
		t.Errorf("forced cleanup failure did not leave reported temp for verification: %v", statErr)
	}
	if failedCleanupPath != "" {
		if removeErr := realRemove(failedCleanupPath); removeErr != nil {
			t.Fatalf("clean reported temporary file: %v", removeErr)
		}
	}
	if got := readFile(t, filepath.Join(root, "cmd", "root.go")); got != rootGo {
		t.Errorf("cmd/root.go changed after staging failure: %q", got)
	}
	if got := readFile(t, filepath.Join(root, "tests", "cli.bats")); got != bats {
		t.Errorf("tests/cli.bats changed after staging failure: %q", got)
	}
	assertNoVersionTemps(t, root)
}

func TestVersionFilesApplyReportsFailedRollbackStagingCleanup(t *testing.T) {
	rootGo := "Version: \"0.5.0\"\n"
	bats := "assert_output \"emberfall version 0.5.0\"\n"
	root := writeVersionFixture(t, rootGo, bats)
	files, err := PrepareVersionFiles(root, Version{Major: 0, Minor: 6, Patch: 0})
	if err != nil {
		t.Fatalf("PrepareVersionFiles: %v", err)
	}

	replaceFailure := errors.New("forced second replacement failure")
	rollbackWriteFailure := errors.New("forced rollback staging write failure")
	rollbackCleanupFailure := errors.New("forced rollback staging cleanup failure")
	realRename := files.ops.rename
	realRemove := files.ops.remove
	realWrite := files.ops.write
	renameCalls := 0
	files.ops.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return replaceFailure
		}
		return realRename(oldPath, newPath)
	}
	writeCalls := 0
	files.ops.write = func(file *os.File, contents []byte) (int, error) {
		writeCalls++
		if writeCalls == 3 {
			return 0, rollbackWriteFailure
		}
		return realWrite(file, contents)
	}
	failedCleanupPath := ""
	removeCalls := 0
	files.ops.remove = func(path string) error {
		removeCalls++
		if removeCalls == 1 {
			failedCleanupPath = path
			return rollbackCleanupFailure
		}
		return realRemove(path)
	}

	err = files.Apply()
	for _, want := range []error{replaceFailure, rollbackWriteFailure, rollbackCleanupFailure} {
		if !errors.Is(err, want) {
			t.Errorf("Apply error = %v, want joined %v", err, want)
		}
	}
	if !strings.Contains(err.Error(), "partial version-file state") || failedCleanupPath == "" || !strings.Contains(err.Error(), failedCleanupPath) {
		t.Errorf("Apply error does not report partial state and leaked rollback temp %q: %v", failedCleanupPath, err)
	}
	if failedCleanupPath != "" {
		if removeErr := realRemove(failedCleanupPath); removeErr != nil {
			t.Fatalf("clean reported rollback temporary file: %v", removeErr)
		}
	}
	if got := readFile(t, filepath.Join(root, "cmd", "root.go")); got != "Version: \"0.6.0\"\n" {
		t.Errorf("cmd/root.go partial state = %q, want explicit updated state", got)
	}
	if got := readFile(t, filepath.Join(root, "tests", "cli.bats")); got != bats {
		t.Errorf("tests/cli.bats changed: %q", got)
	}
	assertNoVersionTemps(t, root)
}

func writeVersionFixture(t *testing.T, rootGo, bats string) string {
	t.Helper()
	root := t.TempDir()
	for path, contents := range map[string]string{
		filepath.Join(root, "cmd", "root.go"):    rootGo,
		filepath.Join(root, "tests", "cli.bats"): bats,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func assertNoVersionTemps(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "*", ".emberfall-version-*"))
	if err != nil {
		t.Fatalf("glob version temps: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("temporary version files leaked: %v", matches)
	}
}
