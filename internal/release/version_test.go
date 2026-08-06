package release

import "testing"

func TestParseVersionAcceptsStableTagsAndLiterals(t *testing.T) {
	tests := []struct {
		input string
		want  Version
	}{
		{input: "0.5.0", want: Version{Major: 0, Minor: 5, Patch: 0}},
		{input: "v1.2.3", want: Version{Major: 1, Minor: 2, Patch: 3}},
		{input: "10.20.30", want: Version{Major: 10, Minor: 20, Patch: 30}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if err != nil {
				t.Fatalf("ParseVersion(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseVersion(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
			if got.String() != tt.want.String() {
				t.Errorf("Version.String() = %q, want %q", got.String(), tt.want.String())
			}
			if got.Tag() != "v"+tt.want.String() {
				t.Errorf("Version.Tag() = %q, want %q", got.Tag(), "v"+tt.want.String())
			}
		})
	}
}

func TestParseVersionRejectsNonStableSemanticVersions(t *testing.T) {
	for _, input := range []string{"", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03", "1.2.3-rc.1", "1.2.3+build", "v-1.2.3", "v1.2.3\n"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseVersion(input); err == nil {
				t.Errorf("ParseVersion(%q) succeeded, want error", input)
			}
		})
	}
}

func TestNextVersionBumpsOnlyRequestedComponent(t *testing.T) {
	current := Version{Major: 1, Minor: 2, Patch: 3}
	tests := []struct {
		bump Bump
		want Version
	}{
		{bump: BumpNone, want: Version{Major: 1, Minor: 2, Patch: 3}},
		{bump: BumpPatch, want: Version{Major: 1, Minor: 2, Patch: 4}},
		{bump: BumpMinor, want: Version{Major: 1, Minor: 3, Patch: 0}},
		{bump: BumpMajor, want: Version{Major: 2, Minor: 0, Patch: 0}},
	}

	for _, tt := range tests {
		t.Run(string(tt.bump), func(t *testing.T) {
			got, err := NextVersion(current, tt.bump)
			if err != nil {
				t.Fatalf("NextVersion(%#v, %q): %v", current, tt.bump, err)
			}
			if got != tt.want {
				t.Errorf("NextVersion(%#v, %q) = %#v, want %#v", current, tt.bump, got, tt.want)
			}
		})
	}
}

func TestNextVersionRejectsUnknownBump(t *testing.T) {
	if _, err := NextVersion(Version{Major: 1}, Bump("surprise")); err == nil {
		t.Error("NextVersion accepted an unknown bump")
	}
}
