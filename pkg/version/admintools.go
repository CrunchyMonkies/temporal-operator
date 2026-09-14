package version

import (
	"fmt"
	"regexp"
)

// DefaultAdminToolTag returns the tag of the admin tools image for the given version.
// It's required as 1.24.x had really bad image tagging.
func DefaultAdminToolTag(version *Version) string {
	// Particular case for >= 1.23.0 but < 1.24.0
	// Need this because 1.23.1 tag does not exist
	if version.GreaterOrEqual(V1_23_0) && version.LessThan(V1_24_0) {
		return "1.23.1.1-tctl-1.18.1-cli-0.12.0"
	}

	// Particular case for >= 1.24.0 but < 1.25.0
	if version.GreaterOrEqual(V1_24_0) && version.LessThan(V1_25_0) {
		return "1.24.2-tctl-1.18.1-cli-1.0.0"
	}

	// Particular case for >= 1.25 because the admin tools image tag doesn't
	// contains patch version (or it has the same bad naming as for 1.24.x).
	if version.GreaterOrEqual(V1_25_0) {
		return fmt.Sprintf("%d.%d", version.Major(), version.Minor())
	}

	return version.String()
}

// historicalDefaultAdminToolTags lists the tags earlier operator releases used as the default
// for a version range, and persisted into spec.admintools.version through the defaulting webhook.
// A stored value matching one of them is the operator's own default, not a user's choice.
var historicalDefaultAdminToolTags = []struct {
	from, to *Version
	tags     []string
}{
	{from: V1_24_0, to: V1_25_0, tags: []string{"1.24.2-tctl-1.18.1-cli-0.13.2"}},
}

// IsDefaultAdminToolTag reports whether tag is what the operator, at any point in its history,
// would have defaulted the admin tools tag to for the given server version.
func IsDefaultAdminToolTag(tag string, version *Version) bool {
	if version == nil || tag == "" {
		return false
	}

	if tag == DefaultAdminToolTag(version) {
		return true
	}

	for _, historical := range historicalDefaultAdminToolTags {
		if !version.GreaterOrEqual(historical.from) || !version.LessThan(historical.to) {
			continue
		}

		for _, candidate := range historical.tags {
			if tag == candidate {
				return true
			}
		}
	}

	return false
}

// knownDefaultAdminToolTag reports whether tag has the shape of a tag the operator defaults to for
// some server version: one of the fixed strings used for the 1.23 and 1.24 lines, past or present,
// or the "major.minor" form used from 1.25 on.
func knownDefaultAdminToolTag(tag string) bool {
	switch tag {
	case "1.23.1.1-tctl-1.18.1-cli-0.12.0", "1.24.2-tctl-1.18.1-cli-0.13.2", "1.24.2-tctl-1.18.1-cli-1.0.0":
		return true
	}

	return majorMinorTag.MatchString(tag)
}

var majorMinorTag = regexp.MustCompile(`^\d+\.\d+$`)

// IsDefaultAdminToolTagForAnotherVersion reports whether tag is a default admin tools tag, but not
// the one for the given server version: a value that was copied from the default of a server
// version the cluster no longer runs, and pins the admin tools there.
func IsDefaultAdminToolTagForAnotherVersion(tag string, version *Version) bool {
	if version == nil || tag == "" || IsDefaultAdminToolTag(tag, version) {
		return false
	}

	return knownDefaultAdminToolTag(tag)
}
