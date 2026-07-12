package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDepsStart(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "deps-start CLI Suite")
}

var _ = Describe("expandVersionArg", func() {
	It("rewrites name@version into a --version flag", func() {
		Expect(expandVersionArg([]string{"deps-start", "postgres@17", "--port", "15432"})).
			To(Equal([]string{"deps-start", "postgres", "--version", "17", "--port", "15432"}))
	})

	It("supports semver constraints like deps install", func() {
		Expect(expandVersionArg([]string{"deps-start", "nats@>=2.10"})).
			To(Equal([]string{"deps-start", "nats", "--version", ">=2.10"}))
	})

	It("leaves plain service names untouched", func() {
		args := []string{"deps-start", "postgres", "-d"}
		Expect(expandVersionArg(args)).To(Equal(args))
	})

	It("leaves subcommands and empty versions untouched", func() {
		Expect(expandVersionArg([]string{"deps-start", "stop", "postgres"})).
			To(Equal([]string{"deps-start", "stop", "postgres"}))
		Expect(expandVersionArg([]string{"deps-start", "postgres@"})).
			To(Equal([]string{"deps-start", "postgres@"}))
	})
})
