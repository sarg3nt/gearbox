package auth

// Constants shared between the build-tagged dev_bypass_on.go and
// dev_bypass_off.go files. Declared here (no build tag) so they survive
// linting even in builds where neither sibling is compiled in.
const (
	// devBypassEnvVar gates whether the dev auto-login bypass is allowed
	// to fire. Even in a binary built with `-tags dev`, the bypass is
	// inert unless this env var is set to "1".
	devBypassEnvVar = "GEARBOX_DEV_AUTO_LOGIN"

	// devBypassEmail is the email/username of the seeded dev account that
	// the bypass auto-authenticates as. The account must exist (and be
	// active) in the users table; the bypass never creates sessions or
	// auto-promotes a non-existent user.
	devBypassEmail = "dev"
)
