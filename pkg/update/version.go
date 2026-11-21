package update

import (
	"context"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// ResolveVersion returns the version string suitable for comparing against
// release tags. For ldflags-set production builds (anything other than the
// literal "dev") it returns the input unchanged. For dev builds it shells
// out to `git describe --tags --always` and appends "-dev" to the output,
// so a build three commits past v1.3.0 becomes "v1.3.0-3-gabcdef-dev" —
// which ParseVersion strips back to 1.3.0 for semantic comparison.
// Returns "dev" unchanged if git describe fails (shallow clone, no tags,
// not run from inside a repo) so that callers can hide the update banner
// rather than nag with a false positive.
func ResolveVersion(rawVersion string) string {
	if rawVersion != "dev" {
		return rawVersion
	}
	res, err := git.Run(context.Background(), git.Request{Args: []string{"describe", "--tags", "--always"}, Env: git.Inherit})
	if err != nil {
		return "dev"
	}
	tag := strings.TrimSpace(string(res.Stdout))
	if tag == "" {
		return "dev"
	}
	return tag + "-dev"
}
