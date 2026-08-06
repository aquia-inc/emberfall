package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/aquia-inc/emberfall/internal/release"
)

const usage = "Usage:\n" +
	"  emberfall-release plan --json\n" +
	"  emberfall-release prepare --github-output PATH\n" +
	"  emberfall-release publish --version X.Y.Z\n" +
	"  emberfall-release notes --tag vX.Y.Z\n" +
	"  emberfall-release enhance-notes --tag vX.Y.Z\n"

type releaseService interface {
	Plan(context.Context) (release.Plan, error)
	Prepare(context.Context, string) (release.Plan, error)
	Publish(context.Context, string) error
	Notes(context.Context, string) (string, error)
	EnhanceNotes(context.Context, release.EnhanceOptions) error
}

func main() {
	service := release.NewService(".", "origin")
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, os.Getenv, service))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, service releaseService) int {
	if len(args) == 0 {
		return usageError(stderr, "a subcommand is required")
	}
	if args[0] == "-h" || args[0] == "--help" {
		if len(args) != 1 {
			return usageError(stderr, "unexpected arguments after --help")
		}
		_, _ = io.WriteString(stdout, usage)
		return 0
	}

	switch args[0] {
	case "plan":
		return runPlan(ctx, args[1:], stdout, stderr, service)
	case "prepare":
		return runPrepare(ctx, args[1:], stdout, stderr, service)
	case "publish":
		return runPublish(ctx, args[1:], stdout, stderr, service)
	case "notes":
		return runNotes(ctx, args[1:], stdout, stderr, service)
	case "enhance-notes":
		return runEnhanceNotes(ctx, args[1:], stdout, stderr, getenv, service)
	default:
		return usageError(stderr, fmt.Sprintf("unknown subcommand %q", args[0]))
	}
}

func runPlan(ctx context.Context, args []string, stdout, stderr io.Writer, service releaseService) int {
	flags := newFlagSet("plan")
	jsonOutput := flags.Bool("json", false, "print the release plan as JSON")
	if code, done := parseFlags(flags, args, stdout, stderr, "  emberfall-release plan --json\n"); done {
		return code
	}
	if !*jsonOutput {
		return usageError(stderr, "--json is required")
	}
	plan, err := service.Plan(ctx)
	if err != nil {
		return commandError(stderr, err)
	}
	if err := json.NewEncoder(stdout).Encode(plan); err != nil {
		return commandError(stderr, fmt.Errorf("write plan JSON: %w", err))
	}
	return 0
}

func runPrepare(ctx context.Context, args []string, stdout, stderr io.Writer, service releaseService) int {
	flags := newFlagSet("prepare")
	output := flags.String("github-output", "", "append release metadata to PATH")
	if code, done := parseFlags(flags, args, stdout, stderr, "  emberfall-release prepare --github-output PATH\n"); done {
		return code
	}
	if *output == "" {
		return usageError(stderr, "--github-output is required")
	}
	if _, err := service.Prepare(ctx, *output); err != nil {
		return commandError(stderr, err)
	}
	return 0
}

func runPublish(ctx context.Context, args []string, stdout, stderr io.Writer, service releaseService) int {
	flags := newFlagSet("publish")
	version := flags.String("version", "", "publish prepared version X.Y.Z")
	if code, done := parseFlags(flags, args, stdout, stderr, "  emberfall-release publish --version X.Y.Z\n"); done {
		return code
	}
	if *version == "" {
		return usageError(stderr, "--version is required")
	}
	if err := service.Publish(ctx, *version); err != nil {
		return commandError(stderr, err)
	}
	return 0
}

func runNotes(ctx context.Context, args []string, stdout, stderr io.Writer, service releaseService) int {
	flags := newFlagSet("notes")
	tag := flags.String("tag", "", "print deterministic notes for vX.Y.Z")
	if code, done := parseFlags(flags, args, stdout, stderr, "  emberfall-release notes --tag vX.Y.Z\n"); done {
		return code
	}
	if *tag == "" {
		return usageError(stderr, "--tag is required")
	}
	notes, err := service.Notes(ctx, *tag)
	if err != nil {
		return commandError(stderr, err)
	}
	if _, err := io.WriteString(stdout, notes); err != nil {
		return commandError(stderr, fmt.Errorf("write release notes: %w", err))
	}
	return 0
}

func runEnhanceNotes(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, service releaseService) int {
	flags := newFlagSet("enhance-notes")
	tag := flags.String("tag", "", "enhance the published release for vX.Y.Z")
	if code, done := parseFlags(flags, args, stdout, stderr, "  emberfall-release enhance-notes --tag vX.Y.Z\n"); done {
		return code
	}
	if *tag == "" {
		return usageError(stderr, "--tag is required")
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	options := release.EnhanceOptions{
		Tag:              *tag,
		AnthropicAPIKey:  getenv("ANTHROPIC_API_KEY"),
		Model:            getenv("RELEASE_NOTES_MODEL"),
		GitHubToken:      getenv("GITHUB_TOKEN"),
		GitHubRepository: getenv("GITHUB_REPOSITORY"),
	}
	if err := service.EnhanceNotes(ctx, options); err != nil {
		return commandError(stderr, err)
	}
	if _, err := fmt.Fprintf(stdout, "enhanced release notes for %s\n", *tag); err != nil {
		return commandError(stderr, fmt.Errorf("write enhancement status: %w", err))
	}
	return 0
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func parseFlags(flags *flag.FlagSet, args []string, stdout, stderr io.Writer, commandUsage string) (int, bool) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, "Usage:\n"+commandUsage)
			return 0, true
		}
		return usageError(stderr, err.Error()), true
	}
	if flags.NArg() != 0 {
		return usageError(stderr, fmt.Sprintf("unexpected arguments: %v", flags.Args())), true
	}
	return 0, false
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "error: %s\n%s", message, usage)
	return 2
}

func commandError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "error: %v\n", err)
	return 1
}
