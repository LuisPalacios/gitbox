// Package update provides update detection, download, and self-update
// capabilities using GitHub Releases as the distribution mechanism.
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version holds parsed semver components.
type Version struct {
	Major int
	Minor int
	Patch int
}

// String returns the version as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ParseVersion extracts major.minor.patch from a version string.
// Accepts formats: "v1.2.3", "1.2.3", "v1.2.3-dev", "v1.2.3 (abc1234)".
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")

	// Strip anything after a space, hyphen-suffix, or plus (pre-release/metadata).
	if idx := strings.IndexAny(s, " -+"); idx != -1 {
		// Keep hyphens that are part of the version (e.g. not "1.2.3-dev")
		part := s[:idx]
		if strings.Count(part, ".") == 2 {
			s = part
		}
	}

	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version %q: expected major.minor.patch", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version %q: %w", parts[0], err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor version %q: %w", parts[1], err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("invalid patch version %q: %w", parts[2], err)
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
}

// IsNewer returns true if latest is strictly newer than current.
func IsNewer(current, latest string) (bool, error) {
	cur, err := ParseVersion(current)
	if err != nil {
		return false, fmt.Errorf("parsing current version: %w", err)
	}
	lat, err := ParseVersion(latest)
	if err != nil {
		return false, fmt.Errorf("parsing latest version: %w", err)
	}

	if lat.Major != cur.Major {
		return lat.Major > cur.Major, nil
	}
	if lat.Minor != cur.Minor {
		return lat.Minor > cur.Minor, nil
	}
	return lat.Patch > cur.Patch, nil
}
