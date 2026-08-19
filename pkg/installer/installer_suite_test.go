package installer

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	postgrestHelperVersionEnv = "DEPS_TEST_POSTGREST_VERSION"
	postgrestHelperMarkerEnv  = "DEPS_TEST_POSTGREST_MARKER"
)

func TestMain(m *testing.M) {
	if version := os.Getenv(postgrestHelperVersionEnv); version != "" {
		if len(os.Args) != 2 || os.Args[1] != "--help" {
			os.Exit(2)
		}
		if marker := os.Getenv(postgrestHelperMarkerEnv); marker != "" {
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				os.Exit(2)
			}
		}
		_, _ = fmt.Fprintf(os.Stdout, "PostgREST %s\nUsage: postgrest FILENAME\n  postgrest --help\n", version)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInstaller(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Installer Suite")
}
