package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aquia-inc/emberfall/internal/release"
)

func TestRunHelpListsExactlyTheAdministrativeCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--help"}, &stdout, &stderr, nil, &fakeReleaseService{})

	if code != 0 {
		t.Fatalf("run help code = %d, want 0; stderr = %q", code, stderr.String())
	}
	want := "Usage:\n" +
		"  emberfall-release plan --json\n" +
		"  emberfall-release prepare --github-output PATH\n" +
		"  emberfall-release publish --version X.Y.Z\n" +
		"  emberfall-release notes --tag vX.Y.Z\n" +
		"  emberfall-release enhance-notes --tag vX.Y.Z\n"
	if stdout.String() != want {
		t.Fatalf("help = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("help stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsMissingFlagsAndExtraArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing command", want: "a subcommand is required"},
		{name: "unknown command", args: []string{"deploy"}, want: "unknown subcommand"},
		{name: "plan requires json", args: []string{"plan"}, want: "--json is required"},
		{name: "prepare output", args: []string{"prepare"}, want: "--github-output is required"},
		{name: "publish version", args: []string{"publish"}, want: "--version is required"},
		{name: "notes tag", args: []string{"notes"}, want: "--tag is required"},
		{name: "enhance tag", args: []string{"enhance-notes"}, want: "--tag is required"},
		{name: "extra argument", args: []string{"plan", "--json", "extra"}, want: "unexpected arguments"},
		{name: "unknown flag", args: []string{"plan", "--json", "--quiet"}, want: "flag provided but not defined"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), test.args, &stdout, &stderr, nil, &fakeReleaseService{})
			if code != 2 {
				t.Fatalf("run(%q) code = %d, want 2", test.args, code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("run(%q) stdout = %q, want empty", test.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("run(%q) stderr = %q, want %q", test.args, stderr.String(), test.want)
			}
		})
	}
}

func TestRunPlanPrintsOneJSONPlan(t *testing.T) {
	service := &fakeReleaseService{plan: release.Plan{
		ReleaseNeeded:   false,
		PreviousVersion: "0.5.0",
		Version:         "0.5.0",
		Tag:             "v0.5.0",
		Bump:            release.BumpNone,
		Commits:         []release.Commit{},
	}}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"plan", "--json"}, &stdout, &stderr, nil, service)

	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("plan code = %d, stderr = %q", code, stderr.String())
	}
	want := "{\"releaseNeeded\":false,\"previousVersion\":\"0.5.0\",\"version\":\"0.5.0\",\"tag\":\"v0.5.0\",\"bump\":\"none\",\"commits\":[]}\n"
	if stdout.String() != want {
		t.Fatalf("plan JSON = %q, want %q", stdout.String(), want)
	}
}

func TestRunDispatchesPreparePublishAndNotes(t *testing.T) {
	service := &fakeReleaseService{notes: "## v0.6.0\n\n### Features\n- ship it\n"}

	t.Run("prepare", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"prepare", "--github-output", "result.txt"}, &stdout, &stderr, nil, service)
		if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("prepare code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
		}
		if service.prepareOutput != "result.txt" {
			t.Fatalf("prepare output path = %q, want result.txt", service.prepareOutput)
		}
	})

	t.Run("publish", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"publish", "--version", "0.6.0"}, &stdout, &stderr, nil, service)
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("publish code = %d, stderr = %q", code, stderr.String())
		}
		if service.publishedVersion != "0.6.0" {
			t.Fatalf("published version = %q, want 0.6.0", service.publishedVersion)
		}
	})

	t.Run("notes", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"notes", "--tag", "v0.6.0"}, &stdout, &stderr, nil, service)
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("notes code = %d, stderr = %q", code, stderr.String())
		}
		if stdout.String() != service.notes {
			t.Fatalf("notes stdout = %q, want exact %q", stdout.String(), service.notes)
		}
	})
}

func TestRunEnhanceNotesPassesEnvironmentWithoutPrintingValues(t *testing.T) {
	service := &fakeReleaseService{}
	environment := map[string]string{
		"ANTHROPIC_API_KEY":   "anthropic-secret",
		"RELEASE_NOTES_MODEL": "test-model",
		"GITHUB_TOKEN":        "github-secret",
		"GITHUB_REPOSITORY":   "aquia-inc/emberfall",
	}
	getenv := func(key string) string { return environment[key] }
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"enhance-notes", "--tag", "v0.6.0"}, &stdout, &stderr, getenv, service)

	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("enhance code = %d, stderr = %q", code, stderr.String())
	}
	want := release.EnhanceOptions{
		Tag:              "v0.6.0",
		AnthropicAPIKey:  "anthropic-secret",
		Model:            "test-model",
		GitHubToken:      "github-secret",
		GitHubRepository: "aquia-inc/emberfall",
	}
	if service.enhanceOptions != want {
		t.Fatalf("enhance options = %#v, want %#v", service.enhanceOptions, want)
	}
	if stdout.String() != "enhanced release notes for v0.6.0\n" {
		t.Fatalf("enhance stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "secret") {
		t.Fatalf("enhance output disclosed configuration: %q", stdout.String()+stderr.String())
	}
}

func TestRunDependencyFailuresReturnOne(t *testing.T) {
	service := &fakeReleaseService{err: errors.New("dependency unavailable")}
	commands := [][]string{
		{"plan", "--json"},
		{"prepare", "--github-output", "result.txt"},
		{"publish", "--version", "0.6.0"},
		{"notes", "--tag", "v0.6.0"},
		{"enhance-notes", "--tag", "v0.6.0"},
	}
	for _, args := range commands {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), args, &stdout, &stderr, func(string) string { return "set" }, service)
		if code != 1 {
			t.Fatalf("run(%q) code = %d, want 1", args, code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "dependency unavailable") {
			t.Fatalf("run(%q) stdout = %q, stderr = %q", args, stdout.String(), stderr.String())
		}
	}
}

type fakeReleaseService struct {
	plan             release.Plan
	notes            string
	err              error
	prepareOutput    string
	publishedVersion string
	enhanceOptions   release.EnhanceOptions
}

func (service *fakeReleaseService) Plan(context.Context) (release.Plan, error) {
	return service.plan, service.err
}

func (service *fakeReleaseService) Prepare(_ context.Context, output string) (release.Plan, error) {
	service.prepareOutput = output
	return service.plan, service.err
}

func (service *fakeReleaseService) Publish(_ context.Context, version string) error {
	service.publishedVersion = version
	return service.err
}

func (service *fakeReleaseService) Notes(context.Context, string) (string, error) {
	return service.notes, service.err
}

func (service *fakeReleaseService) EnhanceNotes(_ context.Context, options release.EnhanceOptions) error {
	service.enhanceOptions = options
	return service.err
}
