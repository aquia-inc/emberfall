package release

import "context"

// Bump identifies the semantic-version component a release changes.
type Bump string

const (
	BumpNone  Bump = "none"
	BumpPatch Bump = "patch"
	BumpMinor Bump = "minor"
	BumpMajor Bump = "major"
)

// Version is a parsed semantic version.
type Version struct {
	Major int
	Minor int
	Patch int
}

// Commit is the conventional-commit information used to plan a release.
type Commit struct {
	Hash     string `json:"hash"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Type     string `json:"type"`
	Scope    string `json:"scope"`
	Breaking bool   `json:"breaking"`
}

// Plan is the machine-readable result of release analysis.
type Plan struct {
	ReleaseNeeded   bool     `json:"releaseNeeded"`
	PreviousVersion string   `json:"previousVersion"`
	Version         string   `json:"version"`
	Tag             string   `json:"tag"`
	Bump            Bump     `json:"bump"`
	Commits         []Commit `json:"commits"`
}

// EnhancementInput provides deterministic release context to a notes enhancer.
type EnhancementInput struct {
	Tag         string
	PreviousTag string
	Notes       string
	Diff        string
	Commits     []Commit
}

// Repository supplies the Git operations needed by release automation.
type Repository interface {
	LatestVersionTag(context.Context) (string, error)
	CommitsSince(context.Context, string) ([]Commit, error)
	EnsureClean(context.Context) error
	CurrentBranch(context.Context) (string, error)
	TagExists(context.Context, string) (bool, error)
	Commit(context.Context, string, ...string) (string, error)
	Tag(context.Context, string) error
	PushAtomic(context.Context, string, string, string) error
}

// Enhancer improves deterministic release notes from release context.
type Enhancer interface {
	Enhance(context.Context, EnhancementInput) (string, error)
}
