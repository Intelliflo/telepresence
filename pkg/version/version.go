package version

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/blang/semver"
)

// Version is a "vSEMVER" string, and is either populated at build-time using `--ldflags -X`, or at
// init()-time by inspecting the binary's own debug info.
var Version string //nolint:gochecknoglobals // constant

func init() {
	// Prefer version number inserted at build using --ldflags, but if it's not set...
	if Version == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			// Fall back to version info from "go get"
			Version = info.Main.Version
		} else {
			Version = "(unknown version)"
		}
		if _, err := semver.ParseTolerant(Version); err != nil {
			if Version != "" && Version != "(devel)" && Version != "(unknown version)" {
				// If this isn't a parsable semver (enforced by Makefile), isn't
				// empty, "(devel)" (a special value from runtime/debug), or our own
				// special "(unknown version)", then something about the toolchain
				// has clearly changed and invalidated our assumptions.  That's
				// worthy of a panic; if this is built using an unsupported
				// compiler, we can't be sure of anything.
				panic(fmt.Errorf("this binary's compiled-in version looks invalid: %w", err))
			}
			if env := os.Getenv("TELEPRESENCE_VERSION"); strings.HasPrefix(env, "v") {
				Version = env
			}
		}
	}
}

// Structured is a structured semver.Version value, and is based on Version.
//
// The reason that this parsed dynamically instead of once at init()-time is so that some
// unit tests can adjust string Version and see that reflected in Structured.
func Structured() semver.Version {
	vs := Version
	switch vs {
	case "(devel)":
		vs = "0.0.0-devel"
	case "(unknown version)":
		vs = "0.0.0-unknownversion"
	}
	v, err := semver.ParseTolerant(vs)
	if err != nil {
		// init() should not have let this happen
		panic(fmt.Errorf("this binary's version is unparsable: %w", err))
	}
	return v
}

func GetExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return executable, nil
}
