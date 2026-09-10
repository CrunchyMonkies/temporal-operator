package version

var (
	// UIV2_0_0 is the first release of the go based temporal ui-server
	// (the temporalio/ui image). Older ui images (temporalio/web 1.x) only read
	// their auth configuration from a config file, they can't be configured
	// using environment variables.
	UIV2_0_0 = MustNewVersionFromString("2.0.0") //nolint:staticcheck,revive
	// UIV2_9_0 is the first ui-server release whose docker config template reads
	// the TEMPORAL_AUTH_SCOPES environment variable. Before that release the
	// requested OIDC scopes were hardcoded in the image's config template.
	UIV2_9_0 = MustNewVersionFromString("2.9.0") //nolint:staticcheck,revive
)

// UISupportsAuth reports whether the provided temporal ui version can have its
// authentication configured using the TEMPORAL_AUTH_* environment variables.
func UISupportsAuth(v *Version) bool {
	return v.GreaterOrEqual(UIV2_0_0)
}

// UISupportsAuthScopes reports whether the provided temporal ui version reads the
// requested OIDC scopes from the TEMPORAL_AUTH_SCOPES environment variable.
func UISupportsAuthScopes(v *Version) bool {
	return v.GreaterOrEqual(UIV2_9_0)
}
