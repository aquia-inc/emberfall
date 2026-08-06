package release

import (
	"fmt"
	"regexp"
	"strconv"
)

var stableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ParseVersion parses a stable semantic-version literal or v-prefixed tag.
func ParseVersion(input string) (Version, error) {
	matches := stableVersion.FindStringSubmatch(input)
	if matches == nil {
		return Version{}, fmt.Errorf("invalid stable semantic version %q", input)
	}

	parts := [3]int{}
	for index := range parts {
		value, err := strconv.Atoi(matches[index+1])
		if err != nil {
			return Version{}, fmt.Errorf("parse semantic version %q: %w", input, err)
		}
		parts[index] = value
	}
	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, nil
}

// String returns the canonical semantic-version literal.
func (version Version) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

// Tag returns the canonical Git tag for the version.
func (version Version) Tag() string {
	return "v" + version.String()
}

// NextVersion applies a release bump to a stable semantic version.
func NextVersion(current Version, bump Bump) (Version, error) {
	if err := validateVersion(current); err != nil {
		return Version{}, err
	}

	switch bump {
	case BumpNone:
		return current, nil
	case BumpPatch:
		return Version{Major: current.Major, Minor: current.Minor, Patch: current.Patch + 1}, nil
	case BumpMinor:
		return Version{Major: current.Major, Minor: current.Minor + 1}, nil
	case BumpMajor:
		return Version{Major: current.Major + 1}, nil
	default:
		return Version{}, fmt.Errorf("unknown release bump %q", bump)
	}
}

func validateVersion(version Version) error {
	if version.Major < 0 || version.Minor < 0 || version.Patch < 0 {
		return fmt.Errorf("invalid negative semantic version %s", version.String())
	}
	return nil
}
