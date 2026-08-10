package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/aquia-inc/emberfall/internal/engine"
	"gopkg.in/yaml.v3"
)

const readmePath = "../README.md"

// readREADME returns the README with line-trailing whitespace removed so
// comparisons do not turn on invisible characters.
func readREADME(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	return trimLineEnds(string(contents))
}

func trimLineEnds(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// helpSchema returns the YAML schema embedded in the root command's long help,
// starting at its "tests:" root.
func helpSchema(t *testing.T) string {
	t.Helper()
	long := trimLineEnds(rootCmd.Long)
	start := strings.Index(long, "\ntests:\n")
	if start < 0 {
		t.Fatal("root command long help no longer contains a tests: schema")
	}
	return strings.TrimSpace(long[start+1:])
}

// yamlBlocks returns the contents of every ```yaml fenced block in markdown.
// An unterminated fence fails the test rather than silently dropping the block
// from coverage.
func yamlBlocks(t *testing.T, markdown string) []string {
	t.Helper()
	var blocks []string
	var current []string
	inBlock := false
	for line := range strings.SplitSeq(markdown, "\n") {
		if !inBlock {
			trimmed := strings.TrimSpace(line)
			if trimmed == "```yaml" || trimmed == "```yml" {
				inBlock = true
				current = nil
			}
			continue
		}
		if strings.TrimSpace(line) == "```" {
			blocks = append(blocks, strings.Join(current, "\n"))
			inBlock = false
			continue
		}
		current = append(current, line)
	}
	if inBlock {
		t.Fatal("README contains an unterminated yaml code fence")
	}
	return blocks
}

// The README's CLI Usage block is copied from cobra, so a new or renamed flag
// has to be reflected in the docs before this passes.
func TestREADMEFlagBlockMatchesCLI(t *testing.T) {
	rootCmd.InitDefaultHelpFlag()
	rootCmd.InitDefaultVersionFlag()

	want := trimLineEnds(rootCmd.Flags().FlagUsages())
	if strings.TrimSpace(want) == "" {
		t.Fatal("cobra reported no flags")
	}
	if !strings.Contains(readREADME(t), want) {
		t.Errorf("README CLI Usage block is out of sync with the registered flags.\nExpected it to contain:\n%s", want)
	}
}

// The README documents the same schema the binary prints, so a schema change in
// one place has to be made in the other.
func TestREADMESchemaMatchesHelp(t *testing.T) {
	want := helpSchema(t)
	for _, block := range yamlBlocks(t, readREADME(t)) {
		if strings.TrimSpace(block) == want {
			return
		}
	}
	t.Errorf("No README yaml block matches the schema printed by --help.\nExpected a block containing:\n%s", want)
}

// Every example config in the README has to unmarshal, so a documented example
// cannot abort at parse time the way the pre-#65 expect.body examples did.
func TestREADMEExampleConfigsParse(t *testing.T) {
	schema := helpSchema(t)
	examples := 0
	for index, block := range yamlBlocks(t, readREADME(t)) {
		// The schema block describes field types rather than values.
		if strings.TrimSpace(block) == schema {
			continue
		}
		if !strings.HasPrefix(block, "tests:") && !strings.Contains(block, "\ntests:") {
			continue
		}
		examples++

		var config engine.Config
		if err := yaml.Unmarshal([]byte(block), &config); err != nil {
			t.Errorf("README yaml block %d does not parse as a tests config: %v\n%s", index, err, block)
			continue
		}
		if len(config.Tests) == 0 {
			t.Errorf("README yaml block %d parsed to zero tests:\n%s", index, block)
		}
	}
	if examples == 0 {
		t.Fatal("found no README example configs to validate")
	}
}

// The GitHub Action examples pin the released version, which on main is the
// version in cmd/root.go, so a release bump that skips the README fails here.
func TestREADMEActionExamplesPinCurrentVersion(t *testing.T) {
	pins := 0
	for index, block := range yamlBlocks(t, readREADME(t)) {
		if !strings.Contains(block, `uses: "aquia-inc/emberfall`) {
			continue
		}
		found := false
		for line := range strings.SplitSeq(block, "\n") {
			trimmed := strings.TrimSpace(line)
			version, ok := strings.CutPrefix(trimmed, "version: ")
			if !ok {
				continue
			}
			found = true
			pins++
			if version != rootCmd.Version {
				t.Errorf("README action example %d pins version %s; the binary is %s", index, version, rootCmd.Version)
			}
		}
		if !found {
			t.Errorf("README action example %d has no version pin:\n%s", index, block)
		}
	}
	if pins == 0 {
		t.Fatal("found no README action examples to validate")
	}
}
