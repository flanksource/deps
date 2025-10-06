package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Wrapper Script Creation", func() {
	var (
		tmpDir    string
		binDir    string
		appDir    string
		installer *Installer
		testTask  *task.Task
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "wrapper-test-*")
		Expect(err).NotTo(HaveOccurred())

		binDir = filepath.Join(tmpDir, "bin")
		appDir = filepath.Join(tmpDir, "opt")

		err = os.MkdirAll(binDir, 0755)
		Expect(err).NotTo(HaveOccurred())
		err = os.MkdirAll(appDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		installer = New(WithBinDir(binDir), WithAppDir(appDir))
		testTask = &task.Task{}
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("createWrapperScript", func() {
		It("should create a wrapper script with templated content", func() {
			pkg := types.Package{
				Name: "test-tool",
				WrapperScript: `#!/bin/bash
java -jar {{.appDir}}/{{.name}}/{{.name}}-{{.version}}.jar "$@"`,
			}

			err := installer.createWrapperScript(pkg, "1.0.0", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			scriptPath := filepath.Join(binDir, "test-tool")
			Expect(scriptPath).To(BeAnExistingFile())

			content, err := os.ReadFile(scriptPath)
			Expect(err).NotTo(HaveOccurred())

			scriptContent := string(content)
			Expect(scriptContent).To(ContainSubstring("#!/bin/bash"))
			Expect(scriptContent).To(ContainSubstring("java -jar"))
			Expect(scriptContent).To(ContainSubstring(appDir + "/test-tool/test-tool-1.0.0.jar"))
			Expect(scriptContent).To(ContainSubstring(`"$@"`))
		})

		It("should make the wrapper script executable", func() {
			pkg := types.Package{
				Name:          "exec-test",
				WrapperScript: "#!/bin/bash\necho 'test'",
			}

			err := installer.createWrapperScript(pkg, "1.0.0", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			scriptPath := filepath.Join(binDir, "exec-test")
			info, err := os.Stat(scriptPath)
			Expect(err).NotTo(HaveOccurred())

			// Check that the file has executable permissions
			mode := info.Mode()
			Expect(mode & 0111).NotTo(BeZero(), "Script should be executable")
		})

		It("should replace existing wrapper script", func() {
			pkg := types.Package{
				Name:          "replace-test",
				WrapperScript: "#!/bin/bash\necho 'original'",
			}

			// Create first version
			err := installer.createWrapperScript(pkg, "1.0.0", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			scriptPath := filepath.Join(binDir, "replace-test")
			content1, _ := os.ReadFile(scriptPath)

			// Update with new version
			pkg.WrapperScript = "#!/bin/bash\necho 'updated'"
			err = installer.createWrapperScript(pkg, "2.0.0", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			content2, _ := os.ReadFile(scriptPath)
			Expect(string(content2)).NotTo(Equal(string(content1)))
			Expect(string(content2)).To(ContainSubstring("updated"))
		})

		It("should skip creation when WrapperScript is empty", func() {
			pkg := types.Package{
				Name:          "no-wrapper",
				WrapperScript: "",
			}

			err := installer.createWrapperScript(pkg, "1.0.0", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			scriptPath := filepath.Join(binDir, "no-wrapper")
			Expect(scriptPath).NotTo(BeAnExistingFile())
		})

		It("should template all available variables", func() {
			pkg := types.Package{
				Name: "template-test",
				WrapperScript: `#!/bin/bash
# AppDir: {{.appDir}}
# BinDir: {{.binDir}}
# Name: {{.name}}
# Version: {{.version}}
# OS: {{.os}}
# Arch: {{.arch}}`,
			}

			err := installer.createWrapperScript(pkg, "3.2.1", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			scriptPath := filepath.Join(binDir, "template-test")
			content, err := os.ReadFile(scriptPath)
			Expect(err).NotTo(HaveOccurred())

			scriptContent := string(content)
			Expect(scriptContent).To(ContainSubstring("AppDir: " + appDir))
			Expect(scriptContent).To(ContainSubstring("BinDir: " + binDir))
			Expect(scriptContent).To(ContainSubstring("Name: template-test"))
			Expect(scriptContent).To(ContainSubstring("Version: 3.2.1"))
			Expect(scriptContent).To(ContainSubstring("OS: "))
			Expect(scriptContent).To(ContainSubstring("Arch: "))
		})

		It("should handle multiline wrapper scripts correctly", func() {
			pkg := types.Package{
				Name: "multiline-test",
				WrapperScript: `#!/bin/bash
set -e
JAVA_OPTS="-Xmx2g"
java $JAVA_OPTS -jar {{.appDir}}/{{.name}}/{{.name}}-{{.version}}.jar "$@"`,
			}

			err := installer.createWrapperScript(pkg, "1.5.0", binDir, testTask)
			Expect(err).NotTo(HaveOccurred())

			scriptPath := filepath.Join(binDir, "multiline-test")
			content, err := os.ReadFile(scriptPath)
			Expect(err).NotTo(HaveOccurred())

			lines := strings.Split(string(content), "\n")
			Expect(len(lines)).To(BeNumerically(">=", 4))
			Expect(lines[0]).To(Equal("#!/bin/bash"))
			Expect(lines[1]).To(Equal("set -e"))
		})
	})
})

func TestWrapperScriptCreation(t *testing.T) {
	t.Run("wrapper script basic functionality", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "wrapper-unit-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		binDir := filepath.Join(tmpDir, "bin")
		appDir := filepath.Join(tmpDir, "opt")
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(appDir, 0755)

		installer := New(WithBinDir(binDir), WithAppDir(appDir))
		testTask := &task.Task{}

		pkg := types.Package{
			Name:          "unit-test-tool",
			WrapperScript: "#!/bin/bash\necho {{.name}}-{{.version}}",
		}

		err = installer.createWrapperScript(pkg, "1.2.3", binDir, testTask)
		if err != nil {
			t.Fatalf("createWrapperScript failed: %v", err)
		}

		scriptPath := filepath.Join(binDir, "unit-test-tool")
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			t.Error("Wrapper script was not created")
		}

		content, _ := os.ReadFile(scriptPath)
		if !strings.Contains(string(content), "unit-test-tool-1.2.3") {
			t.Error("Wrapper script does not contain expected templated content")
		}
	})
}
