package installer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("system-wide installation gate", func() {
	const tool = "cinc-auditor"

	// Tests run without a terminal on stdin, so CanPrompt() is false here and
	// the gate's refusing branch is the one under test. The accepting branch is
	// reached through AssumeYes, which is exactly the opt-in a non-interactive
	// caller has.

	It("refuses when nobody can answer the confirmation", func() {
		subject := New()

		err := subject.checkCanInstallSystemWide(tool)

		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(tool)))
		Expect(err).To(MatchError(ContainSubstring("not a terminal")))
	})

	It("names the flag that opts in", func() {
		// Without this the caller is told the install failed but not what to do
		// about it, and the whole point of failing early is lost.
		subject := New()

		err := subject.checkCanInstallSystemWide(tool)

		Expect(err).To(MatchError(ContainSubstring("--yes")))
	})

	It("proceeds when the caller has assumed yes", func() {
		subject := New(WithAssumeYes(true))

		Expect(subject.checkCanInstallSystemWide(tool)).To(Succeed())
	})

	It("passes the assumption through to the system installer as silence", func() {
		// SystemInstallOptions.Silent is what suppresses the prompt itself;
		// gating without forwarding it would refuse to hang and then hang.
		Expect(New(WithAssumeYes(true)).GetOptions().AssumeYes).To(BeTrue())
		Expect(New().GetOptions().AssumeYes).To(BeFalse())
	})
})
