package cmd

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestServices(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Service aliases")
}

var _ = Describe("service aliases", func() {
	DescribeTable("forwards the full service CLI",
		func(name string) {
			command, args, err := rootCmd.Find([]string{name, "silo", "--runtime", "docker"})
			Expect(err).NotTo(HaveOccurred())
			Expect(command.Name()).To(Equal("start"))
			Expect(command.DisableFlagParsing).To(BeTrue())
			Expect(args).To(Equal([]string{"silo", "--runtime", "docker"}))
		}, Entry("start", "start"), Entry("services", "services"))
})
