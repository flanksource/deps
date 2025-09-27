package maven

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMaven(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Maven Suite")
}