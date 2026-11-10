package version

import (
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
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

// fixedDefaultAdminToolTags are fixed admin tools tags the operator has defaulted to for the
// 1.23 and 1.24 lines, in this release or an earlier one.
var fixedDefaultAdminToolTags = map[string]struct{}{
	"1.23.1.1-tctl-1.18.1-cli-0.12.0": {},
	"1.24.2-tctl-1.18.1-cli-0.13.2":   {},
	"1.24.2-tctl-1.18.1-cli-1.0.0":    {},
}

var majorMinorAdminToolTag = regexp.MustCompile(`^\d+\.\d+$`)

// IsKnownDefaultAdminToolTag reports whether tag has the shape of an admin tools tag the operator
// defaults to for some server version: one of the fixed tags used for the 1.23 and 1.24 lines, the
// "major.minor" form used from 1.25 on, or a plain server version below 1.23, which is what the
// operator used before the admin tools tagging became irregular.
//
// Automatic upgrades treat such a tag as "follow the server version": it is re-resolved through
// DefaultAdminToolTag for every step, instead of pinning the admin tools to the version the tag was
// copied from. Any other tag is a deliberate user choice and is rendered as is.
func IsKnownDefaultAdminToolTag(tag string) bool {
	if knownDefaultAdminToolTag(tag) {
		return true
	}
	v, err := semver.StrictNewVersion(tag)
	if err != nil || v.Prerelease() != "" || v.Metadata() != "" {
		return false
	}
	return v.Major() == 1 && v.Minor() < 23
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

// knownDefaultAdminToolTag reports whether tag is one of the fixed strings used for the 1.23 and
// 1.24 lines, past or present, or the "major.minor" form used from 1.25 on. Unlike
// IsKnownDefaultAdminToolTag it does not count plain pre-1.23 versions, which are indistinguishable
// from a deliberately pinned tag.
func knownDefaultAdminToolTag(tag string) bool {
	if _, ok := fixedDefaultAdminToolTags[tag]; ok {
		return true
	}

	return majorMinorAdminToolTag.MatchString(tag)
}

// IsDefaultAdminToolTagForAnotherVersion reports whether tag is a default admin tools tag, but not
// the one for the given server version: a value that was copied from the default of a server
// version the cluster no longer runs, and pins the admin tools there.
func IsDefaultAdminToolTagForAnotherVersion(tag string, version *Version) bool {
	if version == nil || tag == "" || IsDefaultAdminToolTag(tag, version) {
		return false
	}

	return knownDefaultAdminToolTag(tag)
}
