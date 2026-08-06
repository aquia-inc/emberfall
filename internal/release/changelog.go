package release

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const commitURLPrefix = "https://github.com/aquia-inc/emberfall/commit/"

// RenderChangelog renders deterministic Markdown for the release-relevant commits.
func RenderChangelog(version Version, commits []Commit) string {
	type changelogCommit struct {
		commit Commit
		group  string
		order  int
	}

	unique := make(map[string]Commit, len(commits))
	for _, commit := range commits {
		commit = ParseCommit(commit)
		if current, exists := unique[commit.Hash]; exists && !commitConflictLess(commit, current) {
			continue
		}
		unique[commit.Hash] = commit
	}

	entries := make([]changelogCommit, 0, len(unique))
	for _, commit := range unique {
		group, order := changelogGroup(commit)
		if group == "" {
			continue
		}
		entries = append(entries, changelogCommit{commit: commit, group: group, order: order})
	}

	sort.Slice(entries, func(left, right int) bool {
		if entries[left].order != entries[right].order {
			return entries[left].order < entries[right].order
		}
		if entries[left].commit.Hash != entries[right].commit.Hash {
			return entries[left].commit.Hash < entries[right].commit.Hash
		}
		return entries[left].commit.Subject < entries[right].commit.Subject
	})

	var builder strings.Builder
	fmt.Fprintf(&builder, "## %s\n", version.Tag())
	for index := 0; index < len(entries); {
		group := entries[index].group
		fmt.Fprintf(&builder, "\n### %s\n", group)
		for index < len(entries) && entries[index].group == group {
			commit := entries[index].commit
			fmt.Fprintf(&builder, "- %s ([%s](%s%s))\n", commitDescription(commit), commit.Hash, commitURLPrefix, commit.Hash)
			index++
		}
	}
	return builder.String()
}

// Conflicting records for one immutable hash are resolved independently of input order.
func commitConflictLess(left, right Commit) bool {
	leftValues := [...]string{left.Subject, left.Body, left.Type, left.Scope}
	rightValues := [...]string{right.Subject, right.Body, right.Type, right.Scope}
	for index := range leftValues {
		if leftValues[index] != rightValues[index] {
			return leftValues[index] < rightValues[index]
		}
	}
	return !left.Breaking && right.Breaking
}

func changelogGroup(commit Commit) (string, int) {
	commit = ParseCommit(commit)
	if BumpForCommit(commit) == BumpNone {
		return "", 0
	}
	if commit.Breaking {
		return "Breaking Changes", 0
	}
	switch commit.Type {
	case "feat":
		return "Features", 1
	case "fix":
		return "Fixes", 2
	case "perf":
		return "Performance", 3
	case "docs":
		return "Documentation", 4
	case "refactor":
		return "Refactoring", 5
	default:
		return "", 0
	}
}

func commitDescription(commit Commit) string {
	description := commit.Subject
	if colon := strings.Index(commit.Subject, ": "); colon >= 0 {
		description = commit.Subject[colon+2:]
	}

	var escaped strings.Builder
	for _, character := range description {
		if character == ' ' || unicode.IsLetter(character) || unicode.IsNumber(character) {
			escaped.WriteRune(character)
			continue
		}
		// GitHub linkifies entity-decoded text, so inline boundaries are needed too.
		fmt.Fprintf(&escaped, "<span>&#%d;</span>", character)
	}
	return escaped.String()
}

// PrependChangelog inserts a release section after the changelog title unless its tag already exists.
func PrependChangelog(changelog, section string) (string, error) {
	tag, err := sectionTag(section)
	if err != nil {
		return "", err
	}
	if changelogHasTag(changelog, tag) {
		return changelog, nil
	}
	if changelog == "" {
		return "# Changelog\n\n" + normalizeLineEndings(section, "\n"), nil
	}

	for _, lineEnding := range []string{"\r\n", "\n"} {
		title := "# Changelog" + lineEnding
		if strings.HasPrefix(changelog, title) {
			rest := strings.TrimPrefix(changelog, title)
			rest = strings.TrimPrefix(rest, lineEnding)
			return title + lineEnding + normalizeLineEndings(section, lineEnding) + lineEnding + rest, nil
		}
	}
	return normalizeLineEndings(section, "\n") + "\n" + changelog, nil
}

// ExtractChangelog returns exactly one tagged changelog section.
func ExtractChangelog(changelog, tag string) (string, error) {
	version, err := ParseVersion(tag)
	if err != nil {
		return "", err
	}
	want := "## " + version.Tag()
	headings := markdownHeadings(changelog)
	start := -1
	for index, heading := range headings {
		if heading.text == want {
			start = index
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("changelog section %s not found", version.Tag())
	}
	end := len(changelog)
	for index := start + 1; index < len(headings); index++ {
		if strings.HasPrefix(headings[index].text, "## ") {
			end = headings[index].start
			break
		}
	}
	lineEnding := "\n"
	firstLineEnd := strings.IndexByte(changelog[headings[start].start:], '\n')
	if firstLineEnd > 0 && changelog[headings[start].start+firstLineEnd-1] == '\r' {
		lineEnding = "\r\n"
	}
	return strings.TrimRight(changelog[headings[start].start:end], "\r\n") + lineEnding, nil
}

func sectionTag(section string) (string, error) {
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "## ") {
			return "", fmt.Errorf("changelog section must begin with a version heading")
		}
		version, err := ParseVersion(strings.TrimPrefix(line, "## "))
		if err != nil {
			return "", err
		}
		return version.Tag(), nil
	}
	return "", fmt.Errorf("changelog section is empty")
}

func changelogHasTag(changelog, tag string) bool {
	for _, heading := range markdownHeadings(changelog) {
		if heading.text == "## "+tag {
			return true
		}
	}
	return false
}

func normalizeLineEndings(value, lineEnding string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimRight(value, "\r\n")
	return strings.ReplaceAll(value, "\n", lineEnding) + lineEnding
}

type markdownHeading struct {
	text  string
	start int
}

func markdownHeadings(markdown string) []markdownHeading {
	headings := make([]markdownHeading, 0)
	fenceCharacter := byte(0)
	fenceLength := 0
	for offset := 0; offset < len(markdown); {
		lineEnd := strings.IndexByte(markdown[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(markdown)
		} else {
			lineEnd += offset + 1
		}
		line := strings.TrimSuffix(strings.TrimSuffix(markdown[offset:lineEnd], "\n"), "\r")
		character, length, closing := markdownFence(line, fenceCharacter, fenceLength)
		if fenceCharacter == 0 && character != 0 {
			fenceCharacter = character
			fenceLength = length
		} else if fenceCharacter != 0 && closing {
			fenceCharacter = 0
			fenceLength = 0
		} else if fenceCharacter == 0 && strings.HasPrefix(line, "## ") {
			headings = append(headings, markdownHeading{text: line, start: offset})
		}
		offset = lineEnd
	}
	return headings
}

func markdownFence(line string, openCharacter byte, openLength int) (byte, int, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 {
		return 0, 0, false
	}
	line = line[indent:]
	if line == "" {
		return 0, 0, false
	}
	character := line[0]
	if character != '`' && character != '~' {
		return 0, 0, false
	}
	length := 0
	for length < len(line) && line[length] == character {
		length++
	}
	if length < 3 {
		return 0, 0, false
	}
	if openCharacter == 0 {
		return character, length, false
	}
	closing := character == openCharacter && length >= openLength && strings.TrimSpace(line[length:]) == ""
	return character, length, closing
}
