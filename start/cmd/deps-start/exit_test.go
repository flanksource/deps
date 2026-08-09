package main

import (
	"errors"
	"fmt"
	osexec "os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// exitErrorWith builds the *exec.ExitError a supervisor exiting with code
// produces, by running a shell that exits with it.
func exitErrorWith(code int) error {
	err := osexec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	Expect(err).To(HaveOccurred())
	return err
}

var _ = Describe("supervisorExitCode", func() {
	It("propagates the supervisor's own exit code", func() {
		Expect(supervisorExitCode(exitErrorWith(2))).To(Equal(2))
		Expect(supervisorExitCode(exitErrorWith(17))).To(Equal(17))
	})

	It("reports a failure when the supervisor exited cleanly without becoming ready", func() {
		Expect(supervisorExitCode(nil)).To(Equal(1))
	})

	It("reports a failure for a wait error that carries no exit code", func() {
		Expect(supervisorExitCode(errors.New("signal: killed"))).To(Equal(1))
	})
})

var _ = Describe("startFailure", func() {
	It("carries the exit code through errors.As and keeps the cause", func() {
		cause := errors.New("postgres failed to start, see supervisor.log")
		var err error = &startFailure{code: 3, err: cause}

		var failure *startFailure
		Expect(errors.As(err, &failure)).To(BeTrue())
		Expect(failure.code).To(Equal(3))
		Expect(err.Error()).To(Equal(cause.Error()))
		Expect(errors.Is(err, cause)).To(BeTrue())
	})
})
