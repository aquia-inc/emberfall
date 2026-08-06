package release

import "testing"

func TestParseCommitClassifiesConventionalCommits(t *testing.T) {
	tests := []struct {
		name      string
		commit    Commit
		wantType  string
		wantScope string
		breaking  bool
		wantBump  Bump
	}{
		{name: "feature", commit: Commit{Hash: "a1", Subject: "feat(api): add retries"}, wantType: "feat", wantScope: "api", wantBump: BumpMinor},
		{name: "patch types", commit: Commit{Hash: "a2", Subject: "fix: repair parser"}, wantType: "fix", wantBump: BumpPatch},
		{name: "performance", commit: Commit{Hash: "a3", Subject: "perf(core): reduce allocations"}, wantType: "perf", wantScope: "core", wantBump: BumpPatch},
		{name: "documentation", commit: Commit{Hash: "a4", Subject: "docs: clarify flags"}, wantType: "docs", wantBump: BumpPatch},
		{name: "refactor", commit: Commit{Hash: "a5", Subject: "refactor: simplify parsing"}, wantType: "refactor", wantBump: BumpPatch},
		{name: "ignored chore", commit: Commit{Hash: "a6", Subject: "chore: refresh fixtures"}, wantType: "chore", wantBump: BumpNone},
		{name: "ignored generated release", commit: Commit{Hash: "a7", Subject: "chore(release): bump version to 1.2.3"}, wantType: "chore", wantScope: "release", wantBump: BumpNone},
		{name: "ignored test", commit: Commit{Hash: "a8", Subject: "test: cover edge case"}, wantType: "test", wantBump: BumpNone},
		{name: "ignored ci", commit: Commit{Hash: "a9", Subject: "ci: upgrade runner"}, wantType: "ci", wantBump: BumpNone},
		{name: "unknown conventional type", commit: Commit{Hash: "aa", Subject: "build: update compiler"}, wantType: "build", wantBump: BumpNone},
		{name: "malformed merge", commit: Commit{Hash: "ab", Subject: "Merge pull request #42"}, wantBump: BumpNone},
		{name: "malformed missing colon", commit: Commit{Hash: "ac", Subject: "feat add retries"}, wantBump: BumpNone},
		{name: "malformed C1 control", commit: Commit{Hash: "ad", Subject: "feat: add\u0085retries"}, wantBump: BumpNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCommit(tt.commit)
			if got.Type != tt.wantType || got.Scope != tt.wantScope || got.Breaking != tt.breaking {
				t.Fatalf("ParseCommit(%q) = %#v, want type %q, scope %q, breaking %t", tt.commit.Subject, got, tt.wantType, tt.wantScope, tt.breaking)
			}
			if bump := BumpForCommit(got); bump != tt.wantBump {
				t.Errorf("BumpForCommit(%q) = %q, want %q", tt.commit.Subject, bump, tt.wantBump)
			}
		})
	}
}

func TestParseCommitDetectsBreakingHeadersAndFooters(t *testing.T) {
	tests := []struct {
		name   string
		commit Commit
	}{
		{name: "header", commit: Commit{Subject: "feat(api)!: remove v1 endpoint"}},
		{name: "space footer", commit: Commit{Subject: "fix: reject invalid input", Body: "Details.\n\nBREAKING CHANGE: errors are now typed"}},
		{name: "space-only footer separator", commit: Commit{Subject: "fix: reject invalid input", Body: "Details.\n   \nBREAKING CHANGE: errors are now typed"}},
		{name: "tab-only footer separator", commit: Commit{Subject: "fix: reject invalid input", Body: "Details.\n\t\nBREAKING CHANGE: errors are now typed"}},
		{name: "CRLF whitespace footer separator", commit: Commit{Subject: "fix: reject invalid input", Body: "Details.\r\n \t \r\nBREAKING CHANGE: errors are now typed"}},
		{name: "hyphen footer", commit: Commit{Subject: "docs: update migration guide", Body: "BREAKING-CHANGE: new setup required"}},
		{name: "footer-only multiline value", commit: Commit{Subject: "fix: change API", Body: "BREAKING CHANGE: change return type\nfor all callers"}},
		{name: "multiline footer value", commit: Commit{Subject: "fix: change API", Body: "Details.\n\nBREAKING CHANGE: change return type\nfor all callers"}},
		{name: "multiline footer before another trailer", commit: Commit{Subject: "fix: change API", Body: "Details.\n\nBREAKING-CHANGE: change return type\nfor all callers\nReviewed-by: release team"}},
		{name: "blank line in multiline footer value", commit: Commit{Subject: "fix: change API", Body: "BREAKING CHANGE: change return type\n\nfor all callers\nReviewed-by: release team"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCommit(tt.commit)
			if !got.Breaking {
				t.Fatalf("ParseCommit(%q) did not mark a breaking change", tt.commit.Subject)
			}
			if bump := BumpForCommit(got); bump != BumpMajor {
				t.Errorf("BumpForCommit(%q) = %q, want %q", tt.commit.Subject, bump, BumpMajor)
			}
		})
	}
}

func TestGeneratedReleaseCommitsNeverTriggerAnotherRelease(t *testing.T) {
	tests := []Commit{
		{Subject: "chore(release)!: bump version to 1.2.3"},
		{Subject: "chore(release): bump version to 1.2.3", Body: "BREAKING CHANGE: generated release metadata changed"},
	}
	for _, commit := range tests {
		if got := BumpForCommit(commit); got != BumpNone {
			t.Errorf("BumpForCommit(%q) = %q, want %q", commit.Subject, got, BumpNone)
		}
	}
}

func TestParseCommitRejectsBreakingFooterLookalikes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty value", body: "BREAKING CHANGE:"},
		{name: "missing separator", body: "BREAKING CHANGE:no space"},
		{name: "malformed final footer block", body: "Details.\n\nBREAKING CHANGE:missing separator\nfor all callers"},
		{name: "inline body text", body: "This discusses BREAKING CHANGE: without declaring one."},
		{name: "breaking line embedded in body", body: "Context.\nBREAKING CHANGE: example only\nThis is still body text."},
		{name: "body prose before final footer", body: "Context.\nBREAKING CHANGE: describes an example only\n\nReviewed-by: release team"},
		{name: "space-indented footer only", body: "  BREAKING CHANGE: not a trailer"},
		{name: "tab-indented footer only", body: "\tBREAKING CHANGE: not a trailer"},
		{name: "space-indented footer after leading blank line", body: "\n  BREAKING CHANGE: not a trailer"},
		{name: "tab-indented footer after leading blank line", body: "\n\tBREAKING CHANGE: not a trailer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCommit(Commit{Subject: "fix: keep compatibility", Body: tt.body})
			if got.Breaking {
				t.Errorf("ParseCommit marked body %q as breaking", tt.body)
			}
		})
	}
}

func TestSelectBumpUsesHighestPrecedence(t *testing.T) {
	commits := []Commit{
		ParseCommit(Commit{Subject: "fix: repair parser"}),
		ParseCommit(Commit{Subject: "feat: add command"}),
		ParseCommit(Commit{Subject: "docs: explain command", Body: "\nBREAKING CHANGE: old command removed"}),
	}

	if got := SelectBump(commits); got != BumpMajor {
		t.Errorf("SelectBump() = %q, want %q", got, BumpMajor)
	}
}
