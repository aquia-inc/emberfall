package release

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	conventionalSubject     = regexp.MustCompile(`^([a-z]+)(?:\(([^()\r\n]+)\))?(!)?: ([^\r\n]+)$`)
	breakingFooterLine      = regexp.MustCompile(`^BREAKING(?: |-)CHANGE: [^\t\r\n ].*$`)
	conventionalTrailerLine = regexp.MustCompile(`^(?:BREAKING(?: |-)CHANGE|[A-Za-z][A-Za-z0-9-]*)(?:: | #)[^\t\r\n ].*$`)
	generatedReleaseCommit  = regexp.MustCompile(`^chore\(release\)!?: bump version to (?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)
)

// ParseCommit derives Conventional Commit fields from a commit's subject and body.
// Malformed subjects remain unclassified and therefore cannot trigger a release.
func ParseCommit(commit Commit) Commit {
	commit.Type = ""
	commit.Scope = ""
	commit.Breaking = false

	matches := conventionalSubject.FindStringSubmatch(commit.Subject)
	if matches == nil || strings.IndexFunc(commit.Subject, unicode.IsControl) >= 0 {
		return commit
	}

	commit.Type = matches[1]
	commit.Scope = matches[2]
	commit.Breaking = matches[3] == "!" || hasBreakingFooter(commit.Body)
	return commit
}

// BumpForCommit returns the release impact of one commit.
func BumpForCommit(commit Commit) Bump {
	commit = ParseCommit(commit)
	if commit.Type == "" {
		return BumpNone
	}
	if generatedReleaseCommit.MatchString(commit.Subject) {
		return BumpNone
	}
	if commit.Breaking {
		return BumpMajor
	}

	switch commit.Type {
	case "feat":
		return BumpMinor
	case "fix", "perf", "docs", "refactor":
		return BumpPatch
	default:
		return BumpNone
	}
}

func hasBreakingFooter(body string) bool {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if strings.Trim(body, " \t\n") == "" {
		return false
	}

	lines := strings.Split(body, "\n")
	footerStart := -1
	for i, line := range lines {
		if conventionalTrailerLine.MatchString(line) && (i == 0 || strings.Trim(lines[i-1], " \t") == "") {
			footerStart = i
			break
		}
	}
	if footerStart < 0 {
		return false
	}

	for _, line := range lines[footerStart:] {
		if breakingFooterLine.MatchString(line) {
			return true
		}
	}
	return false
}

// SelectBump returns the highest release impact among commits.
func SelectBump(commits []Commit) Bump {
	bump := BumpNone
	for _, commit := range commits {
		candidate := BumpForCommit(commit)
		if bumpPrecedence(candidate) > bumpPrecedence(bump) {
			bump = candidate
		}
	}
	return bump
}

func bumpPrecedence(bump Bump) int {
	switch bump {
	case BumpMajor:
		return 3
	case BumpMinor:
		return 2
	case BumpPatch:
		return 1
	default:
		return 0
	}
}
