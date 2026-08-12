package release

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlanJSONContract(t *testing.T) {
	plan := Plan{
		ReleaseNeeded:   true,
		PreviousVersion: "0.5.0",
		Version:         "0.6.0",
		Tag:             "v0.6.0",
		Bump:            BumpMinor,
		Commits: []Commit{{
			Hash:     "abc123",
			Subject:  "feat: add release controller",
			Body:     "",
			Type:     "feat",
			Scope:    "release",
			Breaking: false,
		}},
	}

	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	var actual map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}

	want := map[string]json.RawMessage{
		"releaseNeeded":   json.RawMessage("true"),
		"previousVersion": json.RawMessage(`"0.5.0"`),
		"version":         json.RawMessage(`"0.6.0"`),
		"tag":             json.RawMessage(`"v0.6.0"`),
		"bump":            json.RawMessage(`"minor"`),
		"commits":         json.RawMessage(`[{"hash":"abc123","subject":"feat: add release controller","body":"","type":"feat","scope":"release","breaking":false}]`),
	}

	if !reflect.DeepEqual(actual, want) {
		t.Errorf("plan JSON = %s, want fields and values %s", encoded, mustMarshalJSON(t, want))
	}
}

func TestBumpValues(t *testing.T) {
	tests := []struct {
		name string
		got  Bump
		want Bump
	}{
		{name: "none", got: BumpNone, want: "none"},
		{name: "patch", got: BumpPatch, want: "patch"},
		{name: "minor", got: BumpMinor, want: "minor"},
		{name: "major", got: BumpMajor, want: "major"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Bump = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal expected JSON: %v", err)
	}
	return string(encoded)
}

var (
	_     = Version{Major: 1, Minor: 2, Patch: 3}
	_ int = Version{}.Major
	_ int = Version{}.Minor
	_ int = Version{}.Patch

	_ repositoryContract = (Repository)(nil)
	_ Repository         = (repositoryContract)(nil)

	_ func(Repository, context.Context) (string, error)                    = Repository.LatestVersionTag
	_ func(Repository, context.Context, string) ([]Commit, error)          = Repository.CommitsSince
	_ func(Repository, context.Context) error                              = Repository.EnsureClean
	_ func(Repository, context.Context) (string, error)                    = Repository.CurrentBranch
	_ func(Repository, context.Context, string) (bool, error)              = Repository.TagExists
	_ func(Repository, context.Context, string, ...string) (string, error) = Repository.Commit
	_ func(Repository, context.Context, string) error                      = Repository.Tag
	_ func(Repository, context.Context, string, string, string) error      = Repository.PushAtomic

	_ enhancerContract = (Enhancer)(nil)
	_ Enhancer         = (enhancerContract)(nil)

	_ func(Enhancer, context.Context, EnhancementInput) (string, error) = Enhancer.Enhance
)

type repositoryContract interface {
	LatestVersionTag(context.Context) (string, error)
	CommitsSince(context.Context, string) ([]Commit, error)
	EnsureClean(context.Context) error
	CurrentBranch(context.Context) (string, error)
	TagExists(context.Context, string) (bool, error)
	Commit(context.Context, string, ...string) (string, error)
	Tag(context.Context, string) error
	PushAtomic(context.Context, string, string, string) error
}

type enhancerContract interface {
	Enhance(context.Context, EnhancementInput) (string, error)
}
