package state

import (
	"os"
	"testing"
	"time"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/commons-db/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "State Suite")
}

var _ = Describe("State", func() {
	var stateDir string

	BeforeEach(func() {
		stateDir = GinkgoT().TempDir()
	})

	It("round-trips state through save and load", func() {
		st := &State{
			Name:      "postgres",
			Runtime:   "binary",
			Version:   "18.4.0",
			PID:       12345,
			Ports:     map[string]int{"postgres": 15433},
			StartedAt: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC),
			Ready:     true,
			Connection: models.Connection{
				Name:       "postgres",
				Type:       "postgres",
				URL:        "postgres://localhost:15433/postgres",
				Username:   "postgres",
				Password:   "sekret",
				Properties: types.JSONStringMap{"database": "postgres"},
			},
		}
		Expect(st.Save(stateDir)).To(Succeed())

		loaded, err := Load(stateDir, "postgres")
		Expect(err).ToNot(HaveOccurred())
		Expect(loaded).To(Equal(st))
	})

	It("returns os.IsNotExist for never-started services", func() {
		_, err := Load(stateDir, "unknown")
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("lists every saved service and skips empty dirs", func() {
		Expect((&State{Name: "postgres", Runtime: "binary"}).Save(stateDir)).To(Succeed())
		Expect((&State{Name: "valkey", Runtime: "docker"}).Save(stateDir)).To(Succeed())
		_, err := Dir(stateDir, "empty-service")
		Expect(err).ToNot(HaveOccurred())

		states, err := List(stateDir)
		Expect(err).ToNot(HaveOccurred())
		names := []string{states[0].Name, states[1].Name}
		Expect(names).To(ConsistOf("postgres", "valkey"))
	})

	It("deletes state but is a no-op when absent", func() {
		Expect((&State{Name: "postgres"}).Save(stateDir)).To(Succeed())
		Expect(Delete(stateDir, "postgres")).To(Succeed())
		Expect(Delete(stateDir, "postgres")).To(Succeed())
		_, err := Load(stateDir, "postgres")
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})
