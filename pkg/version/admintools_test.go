package version_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
)

func TestDefaultAdminToolTag(t *testing.T) {
	tests := []struct {
		name     string
		version  *version.Version
		expected string
	}{
		{
			name:     "Version 1.23.0",
			version:  version.MustNewVersionFromString("1.23.0"),
			expected: "1.23.1.1-tctl-1.18.1-cli-0.12.0",
		},
		{
			name:     "Version 1.23.9",
			version:  version.MustNewVersionFromString("1.23.9"),
			expected: "1.23.1.1-tctl-1.18.1-cli-0.12.0",
		},
		{
			name:     "Version 1.24.1",
			version:  version.MustNewVersionFromString("1.24.1"),
			expected: "1.24.2-tctl-1.18.1-cli-1.0.0",
		},
		{
			name:     "Version 1.24.9",
			version:  version.MustNewVersionFromString("1.24.9"),
			expected: "1.24.2-tctl-1.18.1-cli-1.0.0",
		},
		{
			name:     "Version 1.25.0",
			version:  version.MustNewVersionFromString("1.25.0"),
			expected: "1.25",
		},
		{
			name:     "Version 1.25.5",
			version:  version.MustNewVersionFromString("1.25.5"),
			expected: "1.25",
		},
		{
			name:     "Version 1.26.0",
			version:  version.MustNewVersionFromString("1.26.0"),
			expected: "1.26",
		},
		{
			name:     "Version 1.10.0",
			version:  version.MustNewVersionFromString("1.10.0"),
			expected: "1.10.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, version.DefaultAdminToolTag(tt.version))
		})
	}
}

func TestIsDefaultAdminToolTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		version  *version.Version
		expected bool
	}{
		{
			name:     "current default for 1.24",
			tag:      "1.24.2-tctl-1.18.1-cli-1.0.0",
			version:  version.MustNewVersionFromString("1.24.3"),
			expected: true,
		},
		{
			name:     "default an earlier release persisted for 1.24",
			tag:      "1.24.2-tctl-1.18.1-cli-0.13.2",
			version:  version.MustNewVersionFromString("1.24.3"),
			expected: true,
		},
		{
			name:     "current default for 1.26",
			tag:      "1.26",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: true,
		},
		{
			name:     "default of another version range",
			tag:      "1.24.2-tctl-1.18.1-cli-1.0.0",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: false,
		},
		{
			name:     "user pinned tag",
			tag:      "1.24.2-tctl-1.18.1-cli-1.0.0-custom",
			version:  version.MustNewVersionFromString("1.24.3"),
			expected: false,
		},
		{
			name:     "empty tag",
			tag:      "",
			version:  version.MustNewVersionFromString("1.24.3"),
			expected: false,
		},
		{
			name:     "nil version",
			tag:      "1.24.2-tctl-1.18.1-cli-1.0.0",
			version:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, version.IsDefaultAdminToolTag(tt.tag, tt.version))
		})
	}
}

func TestIsDefaultAdminToolTagForAnotherVersion(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		version  *version.Version
		expected bool
	}{
		{
			name:     "1.24 default pinned on a 1.25 server",
			tag:      "1.24.2-tctl-1.18.1-cli-1.0.0",
			version:  version.MustNewVersionFromString("1.25.2"),
			expected: true,
		},
		{
			name:     "earlier 1.24 default pinned on a 1.26 server",
			tag:      "1.24.2-tctl-1.18.1-cli-0.13.2",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: true,
		},
		{
			name:     "1.25 default pinned on a 1.26 server",
			tag:      "1.25",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: true,
		},
		{
			name:     "the server version's own default",
			tag:      "1.26",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: false,
		},
		{
			name:     "a tag that is not a default of any version",
			tag:      "1.26.2-custom",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: false,
		},
		{
			name:     "unset",
			tag:      "",
			version:  version.MustNewVersionFromString("1.26.2"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, version.IsDefaultAdminToolTagForAnotherVersion(tt.tag, tt.version))
		})
	}
}
