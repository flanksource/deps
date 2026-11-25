package ghrelease

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGhrelease(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ghrelease")
}
