package envs

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/deps/pkg/template"
)

// RenderEnvs renders environment variable values using template variables
func RenderEnvs(envs map[string]string, data map[string]interface{}) (map[string]string, error) {
	rendered := make(map[string]string)
	for key, valueTemplate := range envs {
		value, err := template.RenderTemplate(valueTemplate, data)
		if err != nil {
			return nil, fmt.Errorf("failed to render env var %s: %w", key, err)
		}
		rendered[key] = value
	}
	return rendered, nil
}

// PrintEnvs prints environment variables to stdout in KEY=value format
func PrintEnvs(envs map[string]string) {
	for key, value := range envs {
		fmt.Printf("%s=%s\n", key, value)
	}
}

// MergeToSystemEnvironment merges environment variables into /etc/environment
func MergeToSystemEnvironment(envs map[string]string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("--system flag requires root privileges (run with sudo)")
	}

	envFilePath := "/etc/environment"

	// Read existing environment file
	existingEnvs := make(map[string]string)
	file, err := os.Open(envFilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", envFilePath, err)
	}

	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				existingEnvs[key] = value
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("failed to parse %s: %w", envFilePath, err)
		}
	}

	// Merge new environment variables
	for key, value := range envs {
		existingEnvs[key] = value
	}

	// Write back to /etc/environment
	tmpFile, err := os.CreateTemp("", "environment-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	writer := bufio.NewWriter(tmpFile)
	for key, value := range existingEnvs {
		if _, err := writer.WriteString(fmt.Sprintf("%s=%s\n", key, value)); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write to temp file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to flush temp file: %w", err)
	}
	tmpFile.Close()

	// Move temp file to /etc/environment
	if err := os.Rename(tmpPath, envFilePath); err != nil {
		return fmt.Errorf("failed to update %s: %w", envFilePath, err)
	}

	if err := os.Chmod(envFilePath, 0644); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", envFilePath, err)
	}

	return nil
}
