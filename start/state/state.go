package state

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/models"
	"gopkg.in/yaml.v3"
)

type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusUnknown Status = "unknown"
)

// State is the persisted record of a started service, written to
// <stateDir>/<name>/state.yaml.
type State struct {
	Name        string            `yaml:"name"`
	Runtime     string            `yaml:"runtime"`
	Version     string            `yaml:"version,omitempty"`
	PID         int               `yaml:"pid,omitempty"`
	ContainerID string            `yaml:"container_id,omitempty"`
	HelmRelease string            `yaml:"helm_release,omitempty"`
	Namespace   string            `yaml:"namespace,omitempty"`
	Commands    map[string]string `yaml:"commands,omitempty"` // command runtime: rendered stop/status
	Ports       map[string]int    `yaml:"ports,omitempty"`    // port name -> host port
	Connection  models.Connection `yaml:"connection"`
	StartedAt   time.Time         `yaml:"started_at"`
	LogFile     string            `yaml:"log_file,omitempty"`
	Ready       bool              `yaml:"ready"`
	// Resources is the latest usage sample: the binary runtime's supervisor
	// persists it periodically, the docker runtime samples on demand.
	Resources *Resources `yaml:"resources,omitempty"`
}

// Resources is a point-in-time usage sample of a running service.
type Resources struct {
	CPUPercent float64   `yaml:"cpu_percent"`
	RSSBytes   uint64    `yaml:"rss_bytes"`
	PeakRSS    uint64    `yaml:"peak_rss,omitempty"`
	OpenFiles  int       `yaml:"open_files,omitempty"`
	Ports      []int     `yaml:"listening_ports,omitempty"` // detected listening ports
	Restarts   int       `yaml:"restarts,omitempty"`
	SampledAt  time.Time `yaml:"sampled_at"`
}

// Dir returns the per-service state directory, creating it if needed.
func Dir(stateDir, name string) (string, error) {
	dir := filepath.Join(stateDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create state dir %s: %w", dir, err)
	}
	return dir, nil
}

func stateFile(stateDir, name string) string {
	return filepath.Join(stateDir, name, "state.yaml")
}

// Load reads the state for a service; os.IsNotExist(err) when never started.
func Load(stateDir, name string) (*State, error) {
	data, err := os.ReadFile(stateFile(stateDir, name))
	if err != nil {
		return nil, err
	}
	var st State
	if err := yaml.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to parse state for %s: %w", name, err)
	}
	return &st, nil
}

// Save atomically writes the state file.
func (s *State) Save(stateDir string) error {
	dir, err := Dir(stateDir, s.Name)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal state for %s: %w", s.Name, err)
	}
	tmp := filepath.Join(dir, ".state.yaml.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, stateFile(stateDir, s.Name))
}

// Delete removes the state file (but keeps data/logs).
func Delete(stateDir, name string) error {
	err := os.Remove(stateFile(stateDir, name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// List returns the state of every service that has been started.
func List(stateDir string) ([]*State, error) {
	entries, err := os.ReadDir(stateDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var states []*State
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		st, err := Load(stateDir, e.Name())
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		states = append(states, st)
	}
	return states, nil
}
