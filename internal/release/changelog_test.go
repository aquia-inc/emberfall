package release

import (
	"html"
	"strings"
	"testing"
)

func TestRenderChangelogSortsEntriesAndLinksCommits(t *testing.T) {
	commits := []Commit{
		ParseCommit(Commit{Hash: "bbbbbbbb", Subject: "fix: zebra bug"}),
		ParseCommit(Commit{Hash: "aaaaaaaa", Subject: "feat: alpha command"}),
		ParseCommit(Commit{Hash: "cccccccc", Subject: "docs: explain setup"}),
		ParseCommit(Commit{Hash: "dddddddd", Subject: "chore: refresh tooling"}),
	}

	got := RenderChangelog(Version{Major: 0, Minor: 6, Patch: 0}, commits)
	want := "## v0.6.0\n\n### Features\n- alpha command ([aaaaaaaa](https://github.com/aquia-inc/emberfall/commit/aaaaaaaa))\n\n### Fixes\n- zebra bug ([bbbbbbbb](https://github.com/aquia-inc/emberfall/commit/bbbbbbbb))\n\n### Documentation\n- explain setup ([cccccccc](https://github.com/aquia-inc/emberfall/commit/cccccccc))\n"
	if got != want {
		t.Errorf("RenderChangelog() =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderChangelogOrdersEntriesByHashWithinAGroup(t *testing.T) {
	commits := []Commit{
		ParseCommit(Commit{Hash: "bbbbbbbb", Subject: "feat: alpha command"}),
		ParseCommit(Commit{Hash: "aaaaaaaa", Subject: "feat: zebra command"}),
	}

	got := RenderChangelog(Version{Major: 0, Minor: 6, Patch: 0}, commits)
	first := "- zebra command ([aaaaaaaa](https://github.com/aquia-inc/emberfall/commit/aaaaaaaa))"
	second := "- alpha command ([bbbbbbbb](https://github.com/aquia-inc/emberfall/commit/bbbbbbbb))"
	firstIndex, secondIndex := strings.Index(got, first), strings.Index(got, second)
	if firstIndex < 0 || firstIndex >= secondIndex {
		t.Errorf("RenderChangelog() did not order same-group commits by hash:\n%s", got)
	}
}

func TestRenderChangelogDeduplicatesHashesWithStableConflictPolicy(t *testing.T) {
	tests := [][]Commit{
		{
			{Hash: "aaaaaaaa", Subject: "feat: zebra command"},
			{Hash: "aaaaaaaa", Subject: "feat: alpha command"},
			{Hash: "aaaaaaaa", Subject: "feat: alpha command"},
		},
		{
			{Hash: "aaaaaaaa", Subject: "feat: alpha command"},
			{Hash: "aaaaaaaa", Subject: "feat: zebra command"},
		},
	}
	for index, commits := range tests {
		got := RenderChangelog(Version{Major: 0, Minor: 6, Patch: 0}, commits)
		if count := strings.Count(got, commitURLPrefix+"aaaaaaaa"); count != 1 {
			t.Errorf("case %d rendered duplicate hash %d times:\n%s", index, count, got)
		}
		if !strings.Contains(got, "- alpha command") || strings.Contains(got, "- zebra command") {
			t.Errorf("case %d did not choose lexicographically smallest conflicting subject:\n%s", index, got)
		}
	}
}

func TestRenderChangelogEscapesUntrustedCommitDescriptions(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
		forbidden   string
	}{
		{name: "ordinary text", description: "plain words", want: "plain words"},
		{name: "Markdown link", description: "[download](https://attacker)", want: "<span>&#91;</span>download<span>&#93;</span><span>&#40;</span>https<span>&#58;</span><span>&#47;</span><span>&#47;</span>attacker<span>&#41;</span>"},
		{name: "Markdown image", description: "![image]", want: "<span>&#33;</span><span>&#91;</span>image<span>&#93;</span>"},
		{name: "HTML comment", description: "<!-- hidden -->", want: "<span>&#60;</span><span>&#33;</span><span>&#45;</span><span>&#45;</span> hidden <span>&#45;</span><span>&#45;</span><span>&#62;</span>"},
		{name: "release notes marker", description: "<!-- emberfall-claude-notes:v1 -->", want: "<span>&#60;</span><span>&#33;</span><span>&#45;</span><span>&#45;</span> emberfall<span>&#45;</span>claude<span>&#45;</span>notes<span>&#58;</span>v1 <span>&#45;</span><span>&#45;</span><span>&#62;</span>", forbidden: "<!-- emberfall-claude-notes:v1 -->"},
		{name: "raw HTML", description: "<img src=x onerror=alert(1)>", want: "<span>&#60;</span>img src<span>&#61;</span>x onerror<span>&#61;</span>alert<span>&#40;</span>1<span>&#41;</span><span>&#62;</span>"},
		{name: "bare links", description: "https://evil.example www.evil.example me@example.com", want: "https<span>&#58;</span><span>&#47;</span><span>&#47;</span>evil<span>&#46;</span>example www<span>&#46;</span>evil<span>&#46;</span>example me<span>&#64;</span>example<span>&#46;</span>com"},
		{name: "GitHub references", description: "notify @octocat about #123", want: "notify <span>&#64;</span>octocat about <span>&#35;</span>123"},
		{name: "headings and lists", description: "# heading - list * bullet > quote", want: "<span>&#35;</span> heading <span>&#45;</span> list <span>&#42;</span> bullet <span>&#62;</span> quote"},
		{name: "code and backslash", description: "use `code` and \\ paths", want: "use <span>&#96;</span>code<span>&#96;</span> and <span>&#92;</span> paths"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const hash = "aaaaaaaa"
			got := RenderChangelog(
				Version{Major: 0, Minor: 6, Patch: 0},
				[]Commit{{Hash: hash, Subject: "feat: " + tt.description}},
			)
			want := "## v0.6.0\n\n### Features\n- " + tt.want + " ([aaaaaaaa](https://github.com/aquia-inc/emberfall/commit/aaaaaaaa))\n"
			if got != want {
				t.Errorf("RenderChangelog() =\n%s\nwant\n%s", got, want)
			}
			visible := strings.ReplaceAll(strings.ReplaceAll(tt.want, "<span>", ""), "</span>", "")
			if unescaped := html.UnescapeString(visible); unescaped != tt.description {
				t.Errorf("escaped description renders as %q, want %q", unescaped, tt.description)
			}
			if tt.forbidden != "" && strings.Contains(got, tt.forbidden) {
				t.Errorf("RenderChangelog() contains forbidden raw marker %q:\n%s", tt.forbidden, got)
			}
		})
	}
}

func TestRenderChangelogOmitsSubjectsWithControlCharacters(t *testing.T) {
	got := RenderChangelog(
		Version{Major: 0, Minor: 6, Patch: 0},
		[]Commit{{Hash: "aaaaaaaa", Subject: "feat: add\u0085retries"}},
	)
	want := "## v0.6.0\n"
	if got != want {
		t.Errorf("RenderChangelog() = %q, want %q", got, want)
	}
}

func TestRenderChangelogGroupsEveryBreakingCommitFirstWithoutDuplication(t *testing.T) {
	commits := []Commit{
		{Hash: "eeeeeeee", Subject: "docs: explain migration"},
		{Hash: "cccccccc", Subject: "build!: replace build pipeline"},
		{Hash: "bbbbbbbb", Subject: "fix!: remove legacy fallback"},
		{Hash: "dddddddd", Subject: "chore!: remove legacy tooling"},
		{Hash: "aaaaaaaa", Subject: "feat!: replace configuration"},
		{Hash: "ffffffff", Subject: "feat: add command"},
		{Hash: "11111111", Subject: "fix: repair state"},
		{Hash: "22222222", Subject: "perf: speed rendering"},
		{Hash: "33333333", Subject: "refactor: simplify policy"},
	}

	got := RenderChangelog(Version{Major: 1}, commits)
	want := "## v1.0.0\n\n### Breaking Changes\n- replace configuration ([aaaaaaaa](https://github.com/aquia-inc/emberfall/commit/aaaaaaaa))\n- remove legacy fallback ([bbbbbbbb](https://github.com/aquia-inc/emberfall/commit/bbbbbbbb))\n- replace build pipeline ([cccccccc](https://github.com/aquia-inc/emberfall/commit/cccccccc))\n- remove legacy tooling ([dddddddd](https://github.com/aquia-inc/emberfall/commit/dddddddd))\n\n### Features\n- add command ([ffffffff](https://github.com/aquia-inc/emberfall/commit/ffffffff))\n\n### Fixes\n- repair state ([11111111](https://github.com/aquia-inc/emberfall/commit/11111111))\n\n### Performance\n- speed rendering ([22222222](https://github.com/aquia-inc/emberfall/commit/22222222))\n\n### Documentation\n- explain migration ([eeeeeeee](https://github.com/aquia-inc/emberfall/commit/eeeeeeee))\n\n### Refactoring\n- simplify policy ([33333333](https://github.com/aquia-inc/emberfall/commit/33333333))\n"
	if got != want {
		t.Errorf("RenderChangelog() =\n%s\nwant\n%s", got, want)
	}
	for _, hash := range []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd", "eeeeeeee", "ffffffff", "11111111", "22222222", "33333333"} {
		if count := strings.Count(got, commitURLPrefix+hash); count != 1 {
			t.Errorf("commit %s rendered %d times, want once:\n%s", hash, count, got)
		}
	}
}

func TestPrependChangelogAddsNewSectionOnce(t *testing.T) {
	section := "## v0.6.0\n\n### Features\n- add release policy ([abc123](https://github.com/aquia-inc/emberfall/commit/abc123))\n"
	existing := "# Changelog\n\n## v0.5.0\n\n- previous release\n"

	got, err := PrependChangelog(existing, section)
	if err != nil {
		t.Fatalf("PrependChangelog: %v", err)
	}
	want := "# Changelog\n\n## v0.6.0\n\n### Features\n- add release policy ([abc123](https://github.com/aquia-inc/emberfall/commit/abc123))\n\n## v0.5.0\n\n- previous release\n"
	if got != want {
		t.Errorf("PrependChangelog() =\n%s\nwant\n%s", got, want)
	}

	again, err := PrependChangelog(got, section)
	if err != nil {
		t.Fatalf("PrependChangelog duplicate: %v", err)
	}
	if again != got {
		t.Errorf("PrependChangelog duplicated a release section:\n%s", again)
	}
}

func TestPrependChangelogPreservesCRLFAndUntouchedContent(t *testing.T) {
	section := "## v0.6.0\n\n### Fixes\n- current\n"
	existing := "# Changelog\r\n\r\n## v0.5.0\r\n\r\n- previous  \r\n"

	got, err := PrependChangelog(existing, section)
	if err != nil {
		t.Fatalf("PrependChangelog: %v", err)
	}
	want := "# Changelog\r\n\r\n## v0.6.0\r\n\r\n### Fixes\r\n- current\r\n\r\n## v0.5.0\r\n\r\n- previous  \r\n"
	if got != want {
		t.Errorf("PrependChangelog CRLF = %q, want %q", got, want)
	}
}

func TestPrependChangelogDoesNotTreatFencedHeadingAsDuplicate(t *testing.T) {
	section := "## v0.6.0\n\n- real release\n"
	existing := "# Changelog\n\n```markdown\n## v0.6.0\nexample only\n```\n\n## v0.5.0\n\n- previous\n"

	got, err := PrependChangelog(existing, section)
	if err != nil {
		t.Fatalf("PrependChangelog: %v", err)
	}
	if !strings.HasPrefix(got, "# Changelog\n\n## v0.6.0\n\n- real release\n") {
		t.Errorf("PrependChangelog did not add a real section before fenced example:\n%s", got)
	}
}

func TestExtractChangelogReturnsOnlyRequestedTag(t *testing.T) {
	changelog := "# Changelog\n\n## v0.6.0\n\n- current\n\n## v0.5.0\n\n- previous\n"
	got, err := ExtractChangelog(changelog, "v0.6.0")
	if err != nil {
		t.Fatalf("ExtractChangelog: %v", err)
	}
	want := "## v0.6.0\n\n- current\n"
	if got != want {
		t.Errorf("ExtractChangelog() = %q, want %q", got, want)
	}

	if _, err := ExtractChangelog(changelog, "v9.9.9"); err == nil {
		t.Error("ExtractChangelog succeeded for a missing tag")
	}
}

func TestExtractChangelogIgnoresHeadingsInsideFences(t *testing.T) {
	changelog := "# Changelog\n\n```markdown\n## v0.6.0\nexample only\n```\n\n## v0.6.0\n\n- current\n\n```markdown\n## v0.5.0\nnot a boundary\n```\n\n## v0.5.0\n\n- previous\n"
	got, err := ExtractChangelog(changelog, "v0.6.0")
	if err != nil {
		t.Fatalf("ExtractChangelog: %v", err)
	}
	want := "## v0.6.0\n\n- current\n\n```markdown\n## v0.5.0\nnot a boundary\n```\n"
	if got != want {
		t.Errorf("ExtractChangelog() = %q, want %q", got, want)
	}

	if _, err := ExtractChangelog("# Changelog\n\n```\n## v0.6.0\n```\n", "v0.6.0"); err == nil {
		t.Error("ExtractChangelog found a tag only present inside a fence")
	}
}
