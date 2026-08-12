package release_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	baselineVersion = "0.5.0"
	releaseVersion  = "0.6.0"
	releaseTag      = "v0.6.0"
	featureSubject  = "feat: exercise compiled release commands"
)

type planJSON struct {
	ReleaseNeeded   bool         `json:"releaseNeeded"`
	PreviousVersion string       `json:"previousVersion"`
	Version         string       `json:"version"`
	Tag             string       `json:"tag"`
	Bump            string       `json:"bump"`
	Commits         []commitJSON `json:"commits"`
}

type commitJSON struct {
	Hash     string `json:"hash"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Type     string `json:"type"`
	Scope    string `json:"scope"`
	Breaking bool   `json:"breaking"`
}

type releaseFixture struct {
	t             *testing.T
	gitPath       string
	goPath        string
	sourceRoot    string
	temporaryRoot string
	repository    string
	remote        string
	artifacts     string
	environment   []string
}

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func TestCompiledReleaseCommandsPublishAtomicallyAndReplanNoRelease(t *testing.T) {
	fixture := newReleaseFixture(t)
	baseline := fixture.git("rev-parse", "HEAD^{commit}")
	fixture.git("tag", "v"+baselineVersion)
	fixture.git("remote", "add", "origin", fixture.remote)
	fixture.git("push", "--atomic", "origin", "refs/heads/main:refs/heads/main", "refs/tags/v"+baselineVersion+":refs/tags/v"+baselineVersion)

	featurePath := filepath.Join(fixture.repository, "acceptance-feature.txt")
	writeFile(t, featurePath, "compiled release acceptance\n", 0o644)
	fixture.git("add", "acceptance-feature.txt")
	fixture.git("commit", "-m", featureSubject)
	featureCommit := fixture.git("rev-parse", "HEAD^{commit}")

	adminBinary := filepath.Join(fixture.artifacts, "emberfall-release")
	fixture.goBuild(adminBinary, "./cmd/emberfall-release")
	assertOutsideRepository(t, fixture.repository, adminBinary)

	plan := fixture.runPlan(adminBinary)
	assertReleasePlan(t, plan, featureCommit)

	originalRoot := readFile(t, filepath.Join(fixture.repository, "cmd", "root.go"))
	originalBATS := readFile(t, filepath.Join(fixture.repository, "tests", "cli.bats"))
	githubOutput := filepath.Join(fixture.artifacts, "github-output")
	assertOutsideRepository(t, fixture.repository, githubOutput)
	fixture.mustCommand(fixture.repository, adminBinary, "prepare", "--github-output", githubOutput)

	wantOutput := "release_needed=true\n" +
		"previous_version=0.5.0\n" +
		"version=0.6.0\n" +
		"tag=v0.6.0\n" +
		"bump=minor\n"
	assertFileEquals(t, githubOutput, wantOutput)
	assertChangedPaths(t, fixture, "CHANGELOG.md", "cmd/root.go", "tests/cli.bats")
	assertSingleLiteralReplacement(t, filepath.Join(fixture.repository, "cmd", "root.go"), originalRoot, `Version: "0.5.0"`, `Version: "0.6.0"`)
	assertSingleLiteralReplacement(t, filepath.Join(fixture.repository, "tests", "cli.bats"), originalBATS, `assert_output "emberfall version 0.5.0"`, `assert_output "emberfall version 0.6.0"`)

	wantNotes := fmt.Sprintf("## v0.6.0\n\n### Features\n- exercise compiled release commands ([%s](https://github.com/aquia-inc/emberfall/commit/%s))\n", featureCommit, featureCommit)
	assertFileEquals(t, filepath.Join(fixture.repository, "CHANGELOG.md"), "# Changelog\n\n"+wantNotes)
	notes := fixture.mustCommand(fixture.repository, adminBinary, "notes", "--tag", releaseTag)
	if notes != wantNotes {
		t.Fatalf("notes output = %q, want %q", notes, wantNotes)
	}

	shippingBinary := filepath.Join(fixture.artifacts, "emberfall")
	fixture.goBuild(shippingBinary, ".")
	assertOutsideRepository(t, fixture.repository, shippingBinary)
	versionOutput := fixture.mustCommand(fixture.repository, shippingBinary, "--version")
	if versionOutput != "emberfall version "+releaseVersion+"\n" {
		t.Fatalf("shipping version output = %q, want %q", versionOutput, "emberfall version "+releaseVersion+"\n")
	}

	fixture.gitDir("update-ref", "refs/tags/"+releaseTag, baseline)
	remoteMainBefore := fixture.gitDir("rev-parse", "refs/heads/main^{commit}")
	remoteTagBefore := fixture.gitDir("rev-parse", "refs/tags/"+releaseTag+"^{commit}")
	firstPublish := fixture.command(fixture.repository, adminBinary, "publish", "--version", releaseVersion)
	if firstPublish.err == nil {
		t.Fatal("first publish succeeded despite the synthetic remote tag collision")
	}
	if got := fixture.gitDir("rev-parse", "refs/heads/main^{commit}"); got != remoteMainBefore {
		t.Fatalf("failed atomic publish changed remote main to %s, want %s", got, remoteMainBefore)
	}
	if got := fixture.gitDir("rev-parse", "refs/tags/"+releaseTag+"^{commit}"); got != remoteTagBefore {
		t.Fatalf("failed atomic publish changed remote tag to %s, want %s", got, remoteTagBefore)
	}

	releaseCommit := fixture.git("rev-parse", "HEAD^{commit}")
	if releaseCommit == featureCommit {
		t.Fatal("failed publish did not retain the generated local release commit")
	}
	if got := fixture.git("rev-parse", "refs/tags/"+releaseTag+"^{commit}"); got != releaseCommit {
		t.Fatalf("retained local tag points to %s, want %s", got, releaseCommit)
	}
	assertReleaseCommit(t, fixture, releaseCommit)

	fixture.gitDir("update-ref", "-d", "refs/tags/"+releaseTag, remoteTagBefore)
	if result := fixture.gitDirCommand("show-ref", "--verify", "--quiet", "refs/tags/"+releaseTag); result.err == nil {
		t.Fatal("synthetic remote collision still exists after its guarded removal")
	}
	if got := fixture.gitDir("rev-parse", "refs/heads/main^{commit}"); got != remoteMainBefore {
		t.Fatalf("removing collision changed remote main to %s, want %s", got, remoteMainBefore)
	}

	fixture.mustCommand(fixture.repository, adminBinary, "publish", "--version", releaseVersion)
	remoteMain := fixture.gitDir("rev-parse", "refs/heads/main^{commit}")
	remoteTag := fixture.gitDir("rev-parse", "refs/tags/"+releaseTag+"^{commit}")
	if remoteMain != releaseCommit || remoteTag != releaseCommit {
		t.Fatalf("published refs do not share release commit %s: main=%s tag=%s", releaseCommit, remoteMain, remoteTag)
	}
	if status := fixture.git("status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("published fixture is dirty: %s", status)
	}

	noRelease := fixture.runPlan(adminBinary)
	if noRelease.ReleaseNeeded || noRelease.PreviousVersion != releaseVersion || noRelease.Version != releaseVersion || noRelease.Tag != releaseTag || noRelease.Bump != "none" || len(noRelease.Commits) != 0 {
		t.Fatalf("post-release plan = %+v, want a no-release plan at %s", noRelease, releaseVersion)
	}
}

func TestHermeticEnvironmentDoesNotInheritCallerVariables(t *testing.T) {
	callerOnlyKeys := []string{
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"ANTHROPIC_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"SSH_AUTH_SOCK",
		"SSH_ASKPASS",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
		"NO_PROXY",
		"EMBERFALL_TEST_CALLER_SECRET",
	}
	for _, key := range callerOnlyKeys {
		t.Setenv(key, "must-not-escape")
	}

	temporaryRoot := t.TempDir()
	environment := hermeticEnvironment(t, temporaryRoot)
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("environment entry %q has no value", entry)
		}
		values[key] = value
	}

	for _, key := range callerOnlyKeys {
		if value, found := values[key]; found {
			t.Errorf("hermetic environment inherited %s=%q", key, value)
		}
	}

	wantPaths := map[string]string{
		"HOME":            filepath.Join(temporaryRoot, "home"),
		"XDG_CONFIG_HOME": filepath.Join(temporaryRoot, "xdg", "config"),
		"XDG_CACHE_HOME":  filepath.Join(temporaryRoot, "xdg", "cache"),
		"XDG_DATA_HOME":   filepath.Join(temporaryRoot, "xdg", "data"),
		"TMPDIR":          filepath.Join(temporaryRoot, "tmp"),
		"TMP":             filepath.Join(temporaryRoot, "tmp"),
		"TEMP":            filepath.Join(temporaryRoot, "tmp"),
	}
	for key, want := range wantPaths {
		if got := values[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func newReleaseFixture(t *testing.T) *releaseFixture {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable: ", err)
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go is unavailable while running Go tests: %v", err)
	}
	sourceRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	fixture := &releaseFixture{
		t:             t,
		gitPath:       gitPath,
		goPath:        goPath,
		sourceRoot:    sourceRoot,
		temporaryRoot: temporaryRoot,
		repository:    filepath.Join(temporaryRoot, "repository"),
		remote:        filepath.Join(temporaryRoot, "remote.git"),
		artifacts:     filepath.Join(temporaryRoot, "artifacts"),
	}
	fixture.environment = hermeticEnvironment(t, temporaryRoot)
	goEnvironment := runCommand(sourceRoot, goCacheDiscoveryEnvironment(temporaryRoot), goPath, "env", "GOMODCACHE", "GOCACHE")
	if goEnvironment.err != nil {
		t.Fatalf("locate existing Go caches: %v\n%s", goEnvironment.err, goEnvironment.stderr)
	}
	goCachePaths := strings.Split(strings.TrimSpace(goEnvironment.stdout), "\n")
	if len(goCachePaths) != 2 {
		t.Fatalf("unexpected go env cache output %q", goEnvironment.stdout)
	}
	fixture.environment = replaceEnvironment(fixture.environment, map[string]string{
		"GOMODCACHE": goCachePaths[0],
		"GOCACHE":    goCachePaths[1],
	})
	if err := os.MkdirAll(fixture.artifacts, 0o755); err != nil {
		t.Fatalf("create artifact directory: %v", err)
	}

	sourceStatus := fixture.gitAtRaw(sourceRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	t.Cleanup(func() {
		if got := fixture.gitAtRaw(sourceRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all"); got != sourceStatus {
			t.Errorf("acceptance test mutated source repository status: before=%q after=%q", sourceStatus, got)
		}
	})
	copyTrackedRepository(t, fixture, sourceRoot, fixture.repository)

	fixture.gitAt(temporaryRoot, "init", "--bare", "--template=", fixture.remote)
	fixture.gitAt(temporaryRoot, "init", "--initial-branch=main", "--template=", fixture.repository)
	emptyHooks := filepath.Join(temporaryRoot, "empty-hooks")
	if err := os.MkdirAll(emptyHooks, 0o755); err != nil {
		t.Fatalf("create empty hooks directory: %v", err)
	}
	fixture.git("config", "--local", "user.name", "Emberfall Release Test")
	fixture.git("config", "--local", "user.email", "release-test@example.invalid")
	fixture.git("config", "--local", "commit.gpgSign", "false")
	fixture.git("config", "--local", "tag.gpgSign", "false")
	fixture.git("config", "--local", "core.hooksPath", emptyHooks)
	fixture.git("config", "--local", "init.templateDir", "")
	fixture.git("config", "--local", "credential.interactive", "never")
	fixture.git("config", "--local", "protocol.file.allow", "always")
	fixture.git("add", "--all")
	fixture.git("commit", "-m", "chore: establish release baseline")

	resolvedSource, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		t.Fatalf("resolve source repository: %v", err)
	}
	resolvedFixture, err := filepath.EvalSymlinks(fixture.git("rev-parse", "--show-toplevel"))
	if err != nil {
		t.Fatalf("resolve fixture repository: %v", err)
	}
	if resolvedFixture == resolvedSource {
		t.Fatalf("fixture repository aliases source repository %s", resolvedSource)
	}
	return fixture
}

func (fixture *releaseFixture) runPlan(adminBinary string) planJSON {
	fixture.t.Helper()
	output := fixture.mustCommand(fixture.repository, adminBinary, "plan", "--json")
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	var plan planJSON
	if err := decoder.Decode(&plan); err != nil {
		fixture.t.Fatalf("decode release plan %q: %v", output, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fixture.t.Fatalf("release plan has trailing JSON: %v", err)
	}
	return plan
}

func assertReleasePlan(t *testing.T, plan planJSON, featureCommit string) {
	t.Helper()
	if !plan.ReleaseNeeded || plan.PreviousVersion != baselineVersion || plan.Version != releaseVersion || plan.Tag != releaseTag || plan.Bump != "minor" {
		t.Fatalf("release plan = %+v, want minor %s -> %s", plan, baselineVersion, releaseVersion)
	}
	if len(plan.Commits) != 1 {
		t.Fatalf("planned commits = %+v, want exactly the feature commit", plan.Commits)
	}
	commit := plan.Commits[0]
	if commit.Hash != featureCommit || commit.Subject != featureSubject || commit.Body != "" || commit.Type != "feat" || commit.Scope != "" || commit.Breaking {
		t.Fatalf("planned commit = %+v, want classified feature %s", commit, featureCommit)
	}
}

func assertReleaseCommit(t *testing.T, fixture *releaseFixture, releaseCommit string) {
	t.Helper()
	wantSubject := "chore(release): bump version to " + releaseVersion
	if subject := fixture.git("log", "-1", "--format=%s", releaseCommit); subject != wantSubject {
		t.Fatalf("release subject = %q, want %q", subject, wantSubject)
	}
	paths := strings.Fields(fixture.git("diff-tree", "--no-commit-id", "--name-only", "-r", releaseCommit))
	wantPaths := []string{"CHANGELOG.md", "cmd/root.go", "tests/cli.bats"}
	if strings.Join(paths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("release paths = %v, want %v", paths, wantPaths)
	}
}

func assertChangedPaths(t *testing.T, fixture *releaseFixture, want ...string) {
	t.Helper()
	status := fixture.gitRaw("status", "--porcelain=v1", "-z", "--untracked-files=all")
	entries := strings.Split(strings.TrimSuffix(status, "\x00"), "\x00")
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			t.Fatalf("unexpected fixture status entry %q", entry)
		}
		paths = append(paths, entry[3:])
	}
	sort.Strings(paths)
	sort.Strings(want)
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("prepared paths = %v, want %v", paths, want)
	}
}

func assertSingleLiteralReplacement(t *testing.T, path, original, oldLiteral, newLiteral string) {
	t.Helper()
	if count := strings.Count(original, oldLiteral); count != 1 {
		t.Fatalf("baseline %s contains %d copies of %q, want 1", path, count, oldLiteral)
	}
	want := strings.Replace(original, oldLiteral, newLiteral, 1)
	assertFileEquals(t, path, want)
}

func copyTrackedRepository(t *testing.T, fixture *releaseFixture, source, destination string) {
	t.Helper()
	tracked := fixture.gitAtRaw(source, "ls-files", "-z")
	for _, relative := range strings.Split(strings.TrimSuffix(tracked, "\x00"), "\x00") {
		if relative == "" {
			continue
		}
		sourcePath := filepath.Join(source, filepath.FromSlash(relative))
		destinationPath := filepath.Join(destination, filepath.FromSlash(relative))
		info, err := os.Lstat(sourcePath)
		if err != nil {
			t.Fatalf("inspect tracked source %s: %v", relative, err)
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			t.Fatalf("create destination for %s: %v", relative, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(sourcePath)
			if err != nil {
				t.Fatalf("read tracked symlink %s: %v", relative, err)
			}
			if err := os.Symlink(target, destinationPath); err != nil {
				t.Fatalf("copy tracked symlink %s: %v", relative, err)
			}
			continue
		}
		contents, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read tracked file %s: %v", relative, err)
		}
		writeFile(t, destinationPath, string(contents), info.Mode().Perm())
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not find repository root from test working directory")
		}
		directory = parent
	}
}

func hermeticEnvironment(t *testing.T, temporaryRoot string) []string {
	t.Helper()
	home := filepath.Join(temporaryRoot, "home")
	xdgConfig := filepath.Join(temporaryRoot, "xdg", "config")
	xdgCache := filepath.Join(temporaryRoot, "xdg", "cache")
	xdgData := filepath.Join(temporaryRoot, "xdg", "data")
	temporaryDirectory := filepath.Join(temporaryRoot, "tmp")
	for _, directory := range []string{home, xdgConfig, xdgCache, xdgData, temporaryDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create hermetic environment directory: %v", err)
		}
	}
	environment := inheritedEnvironment(
		"PATH",
		"GOROOT",
		"SystemRoot",
		"WINDIR",
		"COMSPEC",
		"PATHEXT",
		"SystemDrive",
	)
	return append(environment,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+xdgConfig,
		"XDG_CACHE_HOME="+xdgCache,
		"XDG_DATA_HOME="+xdgData,
		"TMPDIR="+temporaryDirectory,
		"TMP="+temporaryDirectory,
		"TEMP="+temporaryDirectory,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_EDITOR=false",
		"GIT_SEQUENCE_EDITOR=false",
		"GIT_PAGER=cat",
		"GIT_ALLOW_PROTOCOL=file",
		"GCM_INTERACTIVE=Never",
		"LANG=C",
		"LC_ALL=C",
		"PAGER=cat",
	)
}

func goCacheDiscoveryEnvironment(temporaryRoot string) []string {
	environment := inheritedEnvironment(
		"PATH",
		"GOROOT",
		"GOMODCACHE",
		"GOCACHE",
		"GOPATH",
		"HOME",
		"USERPROFILE",
		"XDG_CACHE_HOME",
		"LOCALAPPDATA",
		"SystemRoot",
		"WINDIR",
		"COMSPEC",
		"PATHEXT",
		"SystemDrive",
	)
	return replaceEnvironment(environment, map[string]string{
		"GOENV":       "off",
		"GOPROXY":     "off",
		"GOSUMDB":     "off",
		"GOTOOLCHAIN": "local",
		"TMPDIR":      filepath.Join(temporaryRoot, "tmp"),
		"TMP":         filepath.Join(temporaryRoot, "tmp"),
		"TEMP":        filepath.Join(temporaryRoot, "tmp"),
	})
}

func inheritedEnvironment(keys ...string) []string {
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, found := os.LookupEnv(key); found {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}

func (fixture *releaseFixture) goBuild(output string, packagePath string) {
	fixture.t.Helper()
	environment := replaceEnvironment(fixture.environment, map[string]string{
		"CGO_ENABLED": "0",
		"GOENV":       "off",
		"GOFLAGS":     "-mod=readonly",
		"GOPROXY":     "off",
		"GOSUMDB":     "off",
		"GOTOOLCHAIN": "local",
	})
	result := runCommand(fixture.repository, environment, fixture.goPath, "build", "-o", output, packagePath)
	if result.err != nil {
		fixture.t.Fatalf("build %s: %v\nstdout: %s\nstderr: %s", packagePath, result.err, result.stdout, result.stderr)
	}
}

func replaceEnvironment(environment []string, replacements map[string]string) []string {
	updated := make([]string, 0, len(environment)+len(replacements))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := replacements[key]; !replace {
			updated = append(updated, entry)
		}
	}
	for key, value := range replacements {
		updated = append(updated, key+"="+value)
	}
	return updated
}

func (fixture *releaseFixture) git(arguments ...string) string {
	fixture.t.Helper()
	return fixture.gitAt(fixture.repository, arguments...)
}

func (fixture *releaseFixture) gitRaw(arguments ...string) string {
	fixture.t.Helper()
	return fixture.gitAtRaw(fixture.repository, arguments...)
}

func (fixture *releaseFixture) gitAt(directory string, arguments ...string) string {
	fixture.t.Helper()
	return strings.TrimSpace(fixture.gitAtRaw(directory, arguments...))
}

func (fixture *releaseFixture) gitAtRaw(directory string, arguments ...string) string {
	fixture.t.Helper()
	result := fixture.gitCommandAt(directory, arguments...)
	if result.err != nil {
		fixture.t.Fatalf("git %s: %v\nstdout: %s\nstderr: %s", strings.Join(arguments, " "), result.err, result.stdout, result.stderr)
	}
	return result.stdout
}

func (fixture *releaseFixture) gitDir(arguments ...string) string {
	fixture.t.Helper()
	result := fixture.gitDirCommand(arguments...)
	if result.err != nil {
		fixture.t.Fatalf("git --git-dir %s %s: %v\nstdout: %s\nstderr: %s", fixture.remote, strings.Join(arguments, " "), result.err, result.stdout, result.stderr)
	}
	return strings.TrimSpace(result.stdout)
}

func (fixture *releaseFixture) gitCommandAt(directory string, arguments ...string) commandResult {
	fixture.t.Helper()
	configuration := []string{
		"-c", "core.hooksPath=" + filepath.Join(fixture.temporaryRoot, "empty-hooks"),
		"-c", "init.templateDir=",
		"-c", "credential.interactive=never",
	}
	return runCommand(directory, fixture.environment, fixture.gitPath, append(configuration, arguments...)...)
}

func (fixture *releaseFixture) gitDirCommand(arguments ...string) commandResult {
	fixture.t.Helper()
	withDirectory := append([]string{"--git-dir", fixture.remote}, arguments...)
	return fixture.gitCommandAt(fixture.temporaryRoot, withDirectory...)
}

func (fixture *releaseFixture) command(directory, program string, arguments ...string) commandResult {
	fixture.t.Helper()
	return runCommand(directory, fixture.environment, program, arguments...)
}

func (fixture *releaseFixture) mustCommand(directory, program string, arguments ...string) string {
	fixture.t.Helper()
	result := fixture.command(directory, program, arguments...)
	if result.err != nil {
		fixture.t.Fatalf("%s %s: %v\nstdout: %s\nstderr: %s", program, strings.Join(arguments, " "), result.err, result.stdout, result.stderr)
	}
	return result.stdout
}

func runCommand(directory string, environment []string, program string, arguments ...string) commandResult {
	command := exec.Command(program, arguments...)
	command.Dir = directory
	command.Env = environment
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func assertOutsideRepository(t *testing.T, repository, path string) {
	t.Helper()
	relative, err := filepath.Rel(repository, path)
	if err != nil {
		t.Fatalf("compare repository and artifact path: %v", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		t.Fatalf("artifact %s is inside fixture repository %s", path, repository)
	}
}

func assertFileEquals(t *testing.T, path, want string) {
	t.Helper()
	if got := readFile(t, path); got != want {
		t.Fatalf("contents of %s = %q, want %q", path, got, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func writeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
