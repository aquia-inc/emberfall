package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	githubOwnerPattern      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	githubRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// EnhanceOptions contains the command configuration required to enhance one
// already-published release. Credential values must never be included in
// output or error messages.
type EnhanceOptions struct {
	Tag              string
	AnthropicAPIKey  string
	Model            string
	GitHubToken      string
	GitHubRepository string
}

type releasePublisher interface {
	Publish(context.Context, string) error
}

type releaseUpdater interface {
	EnhanceRelease(context.Context, EnhancementInput) (string, error)
}

type releaseUpdaterFactory func(EnhanceOptions) (releaseUpdater, error)

type serviceFileOperations struct {
	createTemp func(string, string) (*os.File, error)
	write      func(*os.File, []byte) (int, error)
	rename     func(string, string) error
	remove     func(string) error
}

var operatingServiceFiles = serviceFileOperations{
	createTemp: os.CreateTemp,
	write:      (*os.File).Write,
	rename:     os.Rename,
	remove:     os.Remove,
}

// Service coordinates release policy, repository mutations, publication, and
// optional release-note enhancement.
type Service struct {
	directory      string
	remote         string
	repository     Repository
	publisher      releasePublisher
	updaterFactory releaseUpdaterFactory
	files          serviceFileOperations
}

type preparedReleaseFiles struct {
	versionFiles    *VersionFiles
	changelogPath   string
	changelog       []byte
	changelogMode   os.FileMode
	changelogExists bool
}

// NewService constructs the production release service rooted at directory.
func NewService(directory, remote string) *Service {
	repository := NewGitRepository(directory)
	return &Service{
		directory:  directory,
		remote:     remote,
		repository: repository,
		publisher:  NewPublisher(repository, remote),
		files:      operatingServiceFiles,
		updaterFactory: func(options EnhanceOptions) (releaseUpdater, error) {
			owner, repositoryName, err := splitGitHubRepository(options.GitHubRepository)
			if err != nil {
				return nil, err
			}
			httpClient := &http.Client{Timeout: defaultRequestTimeout}
			return ReleaseNotesUpdater{
				Releases: NewGitHubReleaseClient(httpClient, "", options.GitHubToken, owner, repositoryName),
				Enhancer: NewAnthropicEnhancer(httpClient, options.AnthropicAPIKey, options.Model),
			}, nil
		},
	}
}

// Plan computes the next release from the latest reachable version tag and
// Conventional Commits without changing repository state.
func (service *Service) Plan(ctx context.Context) (Plan, error) {
	if service == nil || service.repository == nil {
		return Plan{}, errors.New("release service requires a repository")
	}
	baselineTag, err := service.repository.LatestVersionTag(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("find release baseline: %w", err)
	}
	current, err := ParseVersion(baselineTag)
	if err != nil {
		return Plan{}, fmt.Errorf("parse release baseline %q: %w", baselineTag, err)
	}
	commits, err := service.repository.CommitsSince(ctx, baselineTag)
	if err != nil {
		return Plan{}, fmt.Errorf("read commits after %s: %w", baselineTag, err)
	}
	parsed := make([]Commit, len(commits))
	for index, commit := range commits {
		parsed[index] = ParseCommit(commit)
	}
	bump := SelectBump(parsed)
	next, err := NextVersion(current, bump)
	if err != nil {
		return Plan{}, fmt.Errorf("compute next release version: %w", err)
	}
	return Plan{
		ReleaseNeeded:   bump != BumpNone,
		PreviousVersion: current.String(),
		Version:         next.String(),
		Tag:             next.Tag(),
		Bump:            bump,
		Commits:         parsed,
	}, nil
}

// Prepare requires a clean repository, computes a release plan, updates only
// the managed release files when needed, and finally appends GitHub outputs.
func (service *Service) Prepare(ctx context.Context, githubOutputPath string) (Plan, error) {
	if service == nil || service.repository == nil {
		return Plan{}, errors.New("release service requires a repository")
	}
	if strings.TrimSpace(githubOutputPath) == "" {
		return Plan{}, errors.New("GitHub output path is required")
	}
	if err := service.repository.EnsureClean(ctx); err != nil {
		return Plan{}, fmt.Errorf("prepare release from clean repository: %w", err)
	}
	outputTarget, err := service.validateGitHubOutputTarget(githubOutputPath)
	if err != nil {
		return Plan{}, err
	}
	plan, err := service.Plan(ctx)
	if err != nil {
		return Plan{}, err
	}
	if err := service.validateVersionFileBaseline(plan.PreviousVersion); err != nil {
		return Plan{}, err
	}

	var prepared *preparedReleaseFiles
	if plan.ReleaseNeeded {
		prepared, err = service.prepareReleaseFiles(plan)
		if err != nil {
			return Plan{}, err
		}
	}
	if err := service.replaceGitHubOutput(outputTarget, plan); err != nil {
		if prepared != nil {
			if rollbackErr := prepared.Restore(service); rollbackErr != nil {
				return Plan{}, fmt.Errorf("release files were prepared but GitHub outputs could not be written; partial release-file state after rollback failure: %w", errors.Join(err, rollbackErr))
			}
			return Plan{}, fmt.Errorf("release files were prepared then restored because GitHub outputs could not be written: %w", err)
		}
		return Plan{}, err
	}
	return plan, nil
}

func (service *Service) validateVersionFileBaseline(previousVersion string) error {
	previous, err := ParseVersion(previousVersion)
	if err != nil {
		return fmt.Errorf("parse planned previous version: %w", err)
	}
	files, err := PrepareVersionFiles(service.directory, previous)
	if err != nil {
		return err
	}
	return files.ValidateBaseline(previousVersion)
}

func (service *Service) prepareReleaseFiles(plan Plan) (*preparedReleaseFiles, error) {
	version, err := ParseVersion(plan.Version)
	if err != nil {
		return nil, fmt.Errorf("parse planned version: %w", err)
	}
	versionFiles, err := PrepareVersionFiles(service.directory, version)
	if err != nil {
		return nil, err
	}
	if err := versionFiles.ValidateBaseline(plan.PreviousVersion); err != nil {
		return nil, err
	}

	changelogPath := filepath.Join(service.directory, "CHANGELOG.md")
	_, statErr := os.Lstat(changelogPath)
	changelogExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect %s: %w", changelogPath, statErr)
	}
	changelog, mode, err := readOptionalRegularFile(changelogPath, 0o644)
	if err != nil {
		return nil, err
	}
	section := RenderChangelog(version, plan.Commits)
	updatedChangelog, err := PrependChangelog(string(changelog), section)
	if err != nil {
		return nil, fmt.Errorf("prepare changelog: %w", err)
	}
	temporaryChangelog, err := service.writeTemporaryFile(changelogPath, ".emberfall-changelog-*", []byte(updatedChangelog), mode)
	if err != nil {
		return nil, err
	}

	if err := versionFiles.Apply(); err != nil {
		return nil, service.cleanupTemporary(temporaryChangelog, err)
	}
	if err := service.fileOperations().rename(temporaryChangelog, changelogPath); err != nil {
		replaceErr := fmt.Errorf("replace %s: %w", changelogPath, err)
		if rollbackErr := versionFiles.Restore(); rollbackErr != nil {
			return nil, service.cleanupTemporary(temporaryChangelog, errors.Join(replaceErr, fmt.Errorf("partial release-file state after rollback failure: %w", rollbackErr)))
		}
		return nil, service.cleanupTemporary(temporaryChangelog, replaceErr)
	}
	return &preparedReleaseFiles{
		versionFiles:    versionFiles,
		changelogPath:   changelogPath,
		changelog:       changelog,
		changelogMode:   mode,
		changelogExists: changelogExists,
	}, nil
}

func (prepared *preparedReleaseFiles) Restore(service *Service) error {
	if prepared == nil || prepared.versionFiles == nil {
		return errors.New("no prepared release files")
	}
	versionErr := prepared.versionFiles.Restore()
	changelogErr := prepared.restoreChangelog(service)
	return errors.Join(versionErr, changelogErr)
}

func (prepared *preparedReleaseFiles) restoreChangelog(service *Service) error {
	if !prepared.changelogExists {
		if err := service.fileOperations().remove(prepared.changelogPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove generated %s: %w", prepared.changelogPath, err)
		}
		return nil
	}
	temporary, err := service.writeTemporaryFile(prepared.changelogPath, ".emberfall-changelog-*", prepared.changelog, prepared.changelogMode)
	if err != nil {
		return err
	}
	if err := service.fileOperations().rename(temporary, prepared.changelogPath); err != nil {
		return service.cleanupTemporary(temporary, fmt.Errorf("restore %s: %w", prepared.changelogPath, err))
	}
	return nil
}

// Publish verifies that the requested version is exactly the current canonical
// release plan and prepared file state before delegating the Git transaction.
func (service *Service) Publish(ctx context.Context, version string) error {
	if !stableVersionPattern.MatchString(version) {
		return fmt.Errorf("version %q must be stable X.Y.Z", version)
	}
	if service == nil || service.repository == nil || service.publisher == nil {
		return errors.New("release service requires a publisher")
	}
	retry, err := service.publishExistingRelease(ctx, version)
	if retry {
		return err
	}
	if err != nil {
		return err
	}
	plan, err := service.Plan(ctx)
	if err != nil {
		return err
	}
	if !plan.ReleaseNeeded || plan.Version != version {
		return fmt.Errorf("version %s does not match planned release %s", version, plan.Version)
	}
	if err := service.validatePreparedState(plan); err != nil {
		return err
	}
	return service.publisher.Publish(ctx, version)
}

func (service *Service) publishExistingRelease(ctx context.Context, version string) (bool, error) {
	tag := "v" + version
	exists, err := service.repository.TagExists(ctx, tag)
	if err != nil {
		return false, fmt.Errorf("check release retry tag: %w", err)
	}
	if !exists {
		return false, nil
	}
	if strings.TrimSpace(service.remote) == "" {
		return true, errors.New("release retry requires a configured remote")
	}
	if err := service.repository.EnsureClean(ctx); err != nil {
		return true, fmt.Errorf("release retry requires a clean repository: %w", err)
	}
	branch, err := service.repository.CurrentBranch(ctx)
	if err != nil {
		return true, fmt.Errorf("read release retry branch: %w", err)
	}
	if branch != "main" {
		return true, fmt.Errorf("release retry requires main branch, got %q", branch)
	}

	mainCommit, err := service.runGit(ctx, "rev-parse", "--verify", "refs/heads/main^{commit}")
	if err != nil {
		return true, err
	}
	tagCommit, err := service.runGit(ctx, "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}")
	if err != nil {
		return true, err
	}
	headCommit, err := service.runGit(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return true, err
	}
	if mainCommit != tagCommit || mainCommit != headCommit {
		return true, fmt.Errorf("release retry requires main, HEAD, and tag %s to resolve to the same commit", tag)
	}

	parents, err := service.runGit(ctx, "rev-list", "--parents", "-n", "1", headCommit)
	if err != nil {
		return true, err
	}
	parentFields := strings.Fields(parents)
	if len(parentFields) != 2 || parentFields[0] != headCommit {
		return true, errors.New("release retry requires a single-parent release commit")
	}
	preReleaseCommit := parentFields[1]
	wantSubject := "chore(release): bump version to " + version
	subject, err := service.runGit(ctx, "log", "-1", "--format=%s", headCommit)
	if err != nil {
		return true, err
	}
	if subject != wantSubject {
		return true, fmt.Errorf("release retry HEAD does not have the generated release subject %q", wantSubject)
	}
	if err := service.validateReleaseCommitPaths(ctx, preReleaseCommit, headCommit); err != nil {
		return true, err
	}

	requested, err := ParseVersion(version)
	if err != nil {
		return true, err
	}
	previousTag, err := service.previousVersionTagFromRevision(ctx, preReleaseCommit, requested)
	if err != nil {
		return true, fmt.Errorf("reconstruct release retry baseline: %w", err)
	}
	previous, err := ParseVersion(previousTag)
	if err != nil {
		return true, err
	}
	commits, err := service.commitsBetween(ctx, previousTag, preReleaseCommit)
	if err != nil {
		return true, err
	}
	bump := SelectBump(commits)
	next, err := NextVersion(previous, bump)
	if err != nil {
		return true, err
	}
	if bump == BumpNone || next != requested {
		return true, fmt.Errorf("release retry version %s is not reproduced by commits after %s", version, previousTag)
	}
	plan := Plan{
		ReleaseNeeded:   true,
		PreviousVersion: previous.String(),
		Version:         requested.String(),
		Tag:             requested.Tag(),
		Bump:            bump,
		Commits:         commits,
	}
	if err := service.validatePreparedState(plan); err != nil {
		return true, err
	}
	return true, service.repository.PushAtomic(ctx, service.remote, "main", tag)
}

func (service *Service) validateReleaseCommitPaths(ctx context.Context, parent, releaseCommit string) error {
	output, err := service.runGitRaw(ctx, "diff", "--name-only", "-z", parent, releaseCommit, "--")
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(paths) != len(releasePaths) {
		return errors.New("release retry commit must contain exactly the managed release paths")
	}
	want := make(map[string]bool, len(releasePaths))
	for _, path := range releasePaths {
		want[path] = true
	}
	for _, path := range paths {
		if !want[path] {
			return errors.New("release retry commit must contain exactly the managed release paths")
		}
		delete(want, path)
	}
	if len(want) != 0 {
		return errors.New("release retry commit must contain exactly the managed release paths")
	}
	return nil
}

func (service *Service) validatePreparedState(plan Plan) error {
	want, err := ParseVersion(plan.Version)
	if err != nil {
		return err
	}
	targets := []struct {
		path    string
		matcher interface {
			FindAllSubmatch([]byte, int) [][][]byte
		}
	}{
		{path: filepath.Join(service.directory, "cmd", "root.go"), matcher: rootVersionLiteral},
		{path: filepath.Join(service.directory, "tests", "cli.bats"), matcher: batsVersionLiteral},
	}
	for _, target := range targets {
		contents, err := os.ReadFile(target.path)
		if err != nil {
			return fmt.Errorf("read prepared path %s: %w", target.path, err)
		}
		matches := target.matcher.FindAllSubmatch(contents, -1)
		if len(matches) != 1 || len(matches[0]) < 3 {
			return fmt.Errorf("prepared path %s does not contain exactly one version literal", target.path)
		}
		current, err := ParseVersion(string(matches[0][2]))
		if err != nil || current != want {
			return fmt.Errorf("prepared path %s does not contain version %s", target.path, plan.Version)
		}
	}

	changelog, err := os.ReadFile(filepath.Join(service.directory, "CHANGELOG.md"))
	if err != nil {
		return fmt.Errorf("read prepared changelog: %w", err)
	}
	section, err := ExtractChangelog(string(changelog), plan.Tag)
	if err != nil {
		return fmt.Errorf("validate prepared changelog: %w", err)
	}
	canonical := RenderChangelog(want, plan.Commits)
	if strings.ReplaceAll(section, "\r\n", "\n") != canonical {
		return fmt.Errorf("prepared changelog section %s is not canonical", plan.Tag)
	}
	return nil
}

// Notes extracts exactly the canonical changelog section for tag.
func (service *Service) Notes(_ context.Context, tag string) (string, error) {
	if service == nil {
		return "", errors.New("release service is not configured")
	}
	if _, err := parseCanonicalTag(tag); err != nil {
		return "", err
	}
	contents, err := os.ReadFile(filepath.Join(service.directory, "CHANGELOG.md"))
	if err != nil {
		return "", fmt.Errorf("read changelog: %w", err)
	}
	return ExtractChangelog(string(contents), tag)
}

// EnhanceNotes builds bounded tagged Git context and delegates to production
// Anthropic and GitHub clients. It never returns release-note bodies.
func (service *Service) EnhanceNotes(ctx context.Context, options EnhanceOptions) error {
	currentVersion, err := validateEnhanceOptions(options)
	if err != nil {
		return err
	}
	if service == nil || service.updaterFactory == nil {
		return errors.New("release notes updater is not configured")
	}
	if _, _, err := splitGitHubRepository(options.GitHubRepository); err != nil {
		return err
	}
	previousTag, err := service.previousVersionTag(ctx, options.Tag, currentVersion)
	if err != nil {
		return err
	}
	commits, err := service.commitsBetween(ctx, previousTag, options.Tag)
	if err != nil {
		return err
	}
	diff, err := service.runGit(ctx, "diff", "--no-ext-diff", "--unified=3", previousTag+".."+options.Tag, "--")
	if err != nil {
		return fmt.Errorf("build release diff context: %w", err)
	}
	notes, err := service.Notes(ctx, options.Tag)
	if err != nil {
		return err
	}
	updater, err := service.updaterFactory(options)
	if err != nil {
		return err
	}
	_, err = updater.EnhanceRelease(ctx, EnhancementInput{
		Tag:         options.Tag,
		PreviousTag: previousTag,
		Notes:       notes,
		Diff:        diff,
		Commits:     commits,
	})
	return err
}

func validateEnhanceOptions(options EnhanceOptions) (Version, error) {
	if options.Tag == "" {
		return Version{}, errors.New("--tag is required")
	}
	version, err := parseCanonicalTag(options.Tag)
	if err != nil {
		return Version{}, err
	}
	if strings.TrimSpace(options.AnthropicAPIKey) == "" {
		return Version{}, errors.New("ANTHROPIC_API_KEY is required")
	}
	if strings.TrimSpace(options.GitHubToken) == "" {
		return Version{}, errors.New("GITHUB_TOKEN is required")
	}
	if _, _, err := splitGitHubRepository(options.GitHubRepository); err != nil {
		return Version{}, err
	}
	return version, nil
}

func splitGitHubRepository(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 ||
		len(parts[0]) > 39 || len(parts[1]) > 100 ||
		!githubOwnerPattern.MatchString(parts[0]) || strings.Contains(parts[0], "--") ||
		!githubRepositoryPattern.MatchString(parts[1]) || parts[1] == "." || parts[1] == ".." {
		return "", "", errors.New("GITHUB_REPOSITORY must be owner/repository")
	}
	return parts[0], parts[1], nil
}

func parseCanonicalTag(tag string) (Version, error) {
	version, err := ParseVersion(tag)
	if err != nil || version.Tag() != tag {
		return Version{}, errors.New("--tag must be vX.Y.Z")
	}
	return version, nil
}

func (service *Service) previousVersionTag(ctx context.Context, currentTag string, current Version) (string, error) {
	if _, err := service.runGit(ctx, "rev-parse", "--verify", "refs/tags/"+currentTag+"^{commit}"); err != nil {
		return "", fmt.Errorf("validate release tag %s: %w", currentTag, err)
	}
	return service.previousVersionTagFromRevision(ctx, currentTag, current)
}

func (service *Service) previousVersionTagFromRevision(ctx context.Context, revision string, upperBound Version) (string, error) {
	output, err := service.runGit(ctx, "for-each-ref", "--merged="+revision, "--sort=-version:refname", "--format=%(refname:short)", "refs/tags")
	if err != nil {
		return "", fmt.Errorf("find previous release tag: %w", err)
	}
	for _, candidate := range strings.Fields(output) {
		version, err := ParseVersion(candidate)
		if err == nil && candidate == version.Tag() && versionLess(version, upperBound) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no previous semantic version tag before %s", upperBound.Tag())
}

func versionLess(left, right Version) bool {
	if left.Major != right.Major {
		return left.Major < right.Major
	}
	if left.Minor != right.Minor {
		return left.Minor < right.Minor
	}
	return left.Patch < right.Patch
}

func (service *Service) commitsBetween(ctx context.Context, previousTag, tag string) ([]Commit, error) {
	output, err := service.runGitRaw(ctx, "log", "-z", "--format=%H%x00%s%x00%b", previousTag+".."+tag)
	if err != nil {
		return nil, fmt.Errorf("build release commit context: %w", err)
	}
	fields := strings.Split(output, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%3 != 0 {
		return nil, errors.New("parse release commit context")
	}
	commits := make([]Commit, 0, len(fields)/3)
	for index := 0; index < len(fields); index += 3 {
		commits = append(commits, ParseCommit(Commit{
			Hash:    fields[index],
			Subject: fields[index+1],
			Body:    strings.TrimSuffix(fields[index+2], "\n"),
		}))
	}
	return commits, nil
}

func (service *Service) runGit(ctx context.Context, arguments ...string) (string, error) {
	output, err := service.runGitRaw(ctx, arguments...)
	return strings.TrimSpace(output), err
}

func (service *Service) runGitRaw(ctx context.Context, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = service.directory
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

type releaseOutputValues struct {
	releaseNeeded   bool
	previousVersion string
	version         string
	tag             string
	bump            Bump
}

type githubOutputTarget struct {
	path     string
	original []byte
	mode     os.FileMode
}

func (service *Service) validateGitHubOutputTarget(path string) (githubOutputTarget, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return githubOutputTarget{}, fmt.Errorf("resolve GitHub output path: %w", err)
	}
	info, err := os.Lstat(absolute)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return githubOutputTarget{}, fmt.Errorf("inspect GitHub output: %w", err)
	}
	mode := os.FileMode(0o600)
	var original []byte
	var resolved string
	if exists {
		if !info.Mode().IsRegular() {
			return githubOutputTarget{}, fmt.Errorf("GitHub output %s must be a regular file", path)
		}
		original, err = os.ReadFile(absolute)
		if err != nil {
			return githubOutputTarget{}, fmt.Errorf("read GitHub output: %w", err)
		}
		mode = info.Mode().Perm()
		resolved, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return githubOutputTarget{}, fmt.Errorf("resolve GitHub output: %w", err)
		}
	} else {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(absolute))
		if parentErr != nil {
			return githubOutputTarget{}, fmt.Errorf("resolve GitHub output parent: %w", parentErr)
		}
		parentInfo, parentErr := os.Stat(parent)
		if parentErr != nil || !parentInfo.IsDir() {
			return githubOutputTarget{}, fmt.Errorf("GitHub output parent must be a directory")
		}
		resolved = filepath.Join(parent, filepath.Base(absolute))
	}

	for _, managed := range releasePaths {
		managedPath, pathErr := filepath.Abs(filepath.Join(service.directory, managed))
		if pathErr != nil {
			return githubOutputTarget{}, fmt.Errorf("resolve managed release path: %w", pathErr)
		}
		managedResolved, pathErr := filepath.EvalSymlinks(managedPath)
		if pathErr != nil && !errors.Is(pathErr, os.ErrNotExist) {
			return githubOutputTarget{}, fmt.Errorf("resolve managed release path %s: %w", managed, pathErr)
		}
		if pathErr != nil {
			managedResolved = managedPath
		}
		if filepath.Clean(resolved) == filepath.Clean(managedResolved) {
			return githubOutputTarget{}, fmt.Errorf("GitHub output aliases managed release path %s", managed)
		}
		if exists {
			managedInfo, statErr := os.Stat(managedPath)
			if statErr == nil && os.SameFile(info, managedInfo) {
				return githubOutputTarget{}, fmt.Errorf("GitHub output aliases managed release path %s", managed)
			}
		}
	}
	return githubOutputTarget{path: resolved, original: original, mode: mode}, nil
}

func (service *Service) replaceGitHubOutput(target githubOutputTarget, plan Plan) error {
	payload := githubOutput(releaseOutputValues{
		releaseNeeded:   plan.ReleaseNeeded,
		previousVersion: plan.PreviousVersion,
		version:         plan.Version,
		tag:             plan.Tag,
		bump:            plan.Bump,
	})
	contents := make([]byte, 0, len(target.original)+len(payload))
	contents = append(contents, target.original...)
	contents = append(contents, payload...)
	temporary, err := service.writeTemporaryFile(target.path, ".emberfall-output-*", contents, target.mode)
	if err != nil {
		return err
	}
	if err := service.fileOperations().rename(temporary, target.path); err != nil {
		return service.cleanupTemporary(temporary, fmt.Errorf("replace GitHub output %s: %w", target.path, err))
	}
	return nil
}

func githubOutput(values releaseOutputValues) string {
	return fmt.Sprintf("release_needed=%t\nprevious_version=%s\nversion=%s\ntag=%s\nbump=%s\n",
		values.releaseNeeded, values.previousVersion, values.version, values.tag, values.bump)
}

func readOptionalRegularFile(path string, defaultMode os.FileMode) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, defaultMode, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("changelog target %s must be a regular file", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", path, err)
	}
	return contents, info.Mode().Perm(), nil
}

func (service *Service) writeTemporaryFile(path, pattern string, contents []byte, mode os.FileMode) (string, error) {
	operations := service.fileOperations()
	temporary, err := operations.createTemp(filepath.Dir(path), pattern)
	if err != nil {
		return "", fmt.Errorf("create replacement for %s: %w", path, err)
	}
	name := temporary.Name()
	operationErr := temporary.Chmod(mode.Perm())
	if operationErr == nil {
		written, writeErr := operations.write(temporary, contents)
		if writeErr == nil && written != len(contents) {
			writeErr = io.ErrShortWrite
		}
		operationErr = writeErr
	}
	if operationErr == nil {
		operationErr = temporary.Sync()
	}
	operationErr = errors.Join(operationErr, temporary.Close())
	if operationErr != nil {
		removeErr := operations.remove(name)
		return "", errors.Join(fmt.Errorf("write replacement for %s: %w", path, operationErr), cleanupError(name, removeErr))
	}
	return name, nil
}

func (service *Service) cleanupTemporary(path string, operationErr error) error {
	removeErr := service.fileOperations().remove(path)
	return errors.Join(operationErr, cleanupError(path, removeErr))
}

func (service *Service) fileOperations() serviceFileOperations {
	operations := operatingServiceFiles
	if service != nil {
		if service.files.createTemp != nil {
			operations.createTemp = service.files.createTemp
		}
		if service.files.write != nil {
			operations.write = service.files.write
		}
		if service.files.rename != nil {
			operations.rename = service.files.rename
		}
		if service.files.remove != nil {
			operations.remove = service.files.remove
		}
	}
	return operations
}
