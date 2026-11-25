//go:build unix

// Tests that a binary service's recorded process is its own: a PID alone can name another process
// once the service's state outlives it, as a state restored from a CI cache or kept over a reboot.
package start

import (
	"context"
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/start/state"
)

var _ = Describe("a binary service's recorded process", func() {
	status := func(st *state.State) state.Status {
		GinkgoHelper()
		status, err := (&binaryRuntime{}).Status(context.Background(), st)
		Expect(err).NotTo(HaveOccurred())
		return status
	}

	// A service's process starts before deps-start records the service, as this test's did.
	It("is running when the process with its PID started before the service was recorded", func() {
		Expect(status(&state.State{PID: os.Getpid(), StartedAt: time.Now()})).To(Equal(state.StatusRunning))
	})

	It("is stopped when the process with its PID started after the service was recorded", func() {
		Expect(status(&state.State{PID: os.Getpid(), StartedAt: time.Now().Add(-24 * time.Hour)})).To(Equal(state.StatusStopped))
	})

	It("is stopped when no process has its PID", func() {
		exited := exec.Command("true")
		Expect(exited.Run()).To(Succeed())
		Expect(status(&state.State{PID: exited.Process.Pid, StartedAt: time.Now()})).To(Equal(state.StatusStopped))
	})

	It("is its supervisor only when that process started before the service was recorded", func() {
		Expect(serviceProcessAlive(os.Getpid(), time.Now())).To(BeTrue())
		Expect(serviceProcessAlive(os.Getpid(), time.Now().Add(-24*time.Hour))).To(BeFalse())
	})
})
