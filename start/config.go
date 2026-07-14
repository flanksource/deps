package start

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/flanksource/deps/start/state"
)

const managedConfigPrefix = "deps-start:"

type ConfigChange struct {
	Before string
	After  string
}

func NewConfigChange(live, desired *state.EffectiveConfig) (*ConfigChange, error) {
	if reflect.DeepEqual(live, desired) {
		return nil, nil
	}
	before, err := marshalEffectiveConfig(live)
	if err != nil {
		return nil, err
	}
	after, err := marshalEffectiveConfig(desired)
	if err != nil {
		return nil, err
	}
	return &ConfigChange{Before: before, After: after}, nil
}

func marshalEffectiveConfig(config *state.EffectiveConfig) (string, error) {
	if config == nil {
		return "", nil
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal effective service config: %w", err)
	}
	return string(data), nil
}

func encodeManagedConfig(config *state.EffectiveConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to encode managed service config: %w", err)
	}
	return managedConfigPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeManagedConfig(value string) (*state.EffectiveConfig, error) {
	if !strings.HasPrefix(value, managedConfigPrefix) {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, managedConfigPrefix))
	if err != nil {
		return nil, fmt.Errorf("invalid managed service config encoding: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config state.EffectiveConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("invalid managed service config: %w", err)
	}
	return &config, nil
}

func configuredPorts(svc *ServiceContext) map[string]int {
	ports := map[string]int{}
	primary, _ := svc.Spec.PrimaryPort()
	for _, port := range svc.Spec.Ports {
		ports[port.Name] = port.Port
		if port.Name == primary.Name && svc.Opts.Port != 0 {
			ports[port.Name] = svc.Opts.Port
		}
	}
	return ports
}

func cloneStrings(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
