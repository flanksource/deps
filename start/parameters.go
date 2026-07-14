package start

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/flanksource/deps/pkg/types"
)

var serviceParameterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func ValidateServiceParameters(spec types.ServiceSpec) error {
	supportedRuntimes := make(map[string]struct{}, len(spec.Runtimes()))
	for _, runtime := range spec.Runtimes() {
		supportedRuntimes[runtime] = struct{}{}
	}
	for _, name := range sortedParameterNames(spec.Parameters) {
		parameter := spec.Parameters[name]
		if !serviceParameterNamePattern.MatchString(name) {
			return fmt.Errorf("invalid parameter name %q: expected lowercase kebab-case", name)
		}
		if strings.TrimSpace(parameter.Description) == "" {
			return fmt.Errorf("parameter --%s requires a description", name)
		}
		for _, runtime := range parameter.Runtimes {
			if _, ok := supportedRuntimes[runtime]; !ok {
				return fmt.Errorf("parameter --%s references unsupported runtime %q", name, runtime)
			}
		}
		switch parameter.Type {
		case types.ServiceParameterString, types.ServiceParameterQuantity:
		case types.ServiceParameterBool, types.ServiceParameterInt, types.ServiceParameterDuration:
			if parameter.Default == "" {
				return fmt.Errorf("parameter --%s of type %s requires a default", name, parameter.Type)
			}
		default:
			return fmt.Errorf("parameter --%s has unsupported type %q", name, parameter.Type)
		}
		if parameter.Pattern != "" {
			if _, err := regexp.Compile(parameter.Pattern); err != nil {
				return fmt.Errorf("parameter --%s has invalid pattern: %w", name, err)
			}
		}
		if parameter.Default != "" {
			if _, _, err := parseServiceParameter(name, parameter, parameter.Default); err != nil {
				return fmt.Errorf("invalid default for --%s: %w", name, err)
			}
		}
	}
	return nil
}

func resolveServiceParameters(spec types.ServiceSpec, runtime RuntimeKind, supplied map[string]string) (map[string]any, map[string]string, error) {
	if err := validateSuppliedServiceParameters(spec, supplied); err != nil {
		return nil, nil, err
	}

	values := make(map[string]any, len(spec.Parameters))
	persisted := make(map[string]string, len(spec.Parameters))
	for _, name := range sortedParameterNames(spec.Parameters) {
		definition := spec.Parameters[name]
		raw, explicit := supplied[name]
		if !explicit {
			raw = definition.Default
		}
		if raw == "" {
			values[name] = ""
			continue
		}
		if !parameterSupportsRuntime(definition, runtime) {
			if explicit {
				return nil, nil, fmt.Errorf("parameter --%s only applies to %s runtime", name, strings.Join(definition.Runtimes, ", "))
			}
			values[name] = ""
			continue
		}
		value, canonical, err := parseServiceParameter(name, definition, raw)
		if err != nil {
			return nil, nil, err
		}
		values[name] = value
		persisted[name] = canonical
	}
	return values, persisted, nil
}

func validateSuppliedServiceParameters(spec types.ServiceSpec, supplied map[string]string) error {
	if err := ValidateServiceParameters(spec); err != nil {
		return err
	}
	for name, raw := range supplied {
		definition, ok := spec.Parameters[name]
		if !ok {
			return fmt.Errorf("unknown parameter %q", name)
		}
		if raw != "" {
			if _, _, err := parseServiceParameter(name, definition, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedParameterNames(parameters map[string]types.ServiceParameter) []string {
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func parameterSupportsRuntime(parameter types.ServiceParameter, runtime RuntimeKind) bool {
	if len(parameter.Runtimes) == 0 {
		return true
	}
	for _, candidate := range parameter.Runtimes {
		if candidate == string(runtime) {
			return true
		}
	}
	return false
}

func parseServiceParameter(name string, parameter types.ServiceParameter, raw string) (any, string, error) {
	if parameter.Pattern != "" {
		pattern, err := regexp.Compile(parameter.Pattern)
		if err != nil {
			return nil, "", fmt.Errorf("parameter --%s has invalid pattern: %w", name, err)
		}
		if !pattern.MatchString(raw) {
			return nil, "", fmt.Errorf("invalid value %q for --%s", raw, name)
		}
	}

	switch parameter.Type {
	case types.ServiceParameterString:
		return raw, raw, nil
	case types.ServiceParameterBool:
		value, err := strconv.ParseBool(raw)
		return value, strconv.FormatBool(value), parameterError(name, raw, err)
	case types.ServiceParameterInt:
		value, err := strconv.Atoi(raw)
		return value, strconv.Itoa(value), parameterError(name, raw, err)
	case types.ServiceParameterDuration:
		value, err := time.ParseDuration(raw)
		return value, value.String(), parameterError(name, raw, err)
	case types.ServiceParameterQuantity:
		_, err := resource.ParseQuantity(raw)
		return raw, raw, parameterError(name, raw, err)
	default:
		return nil, "", fmt.Errorf("parameter --%s has unsupported type %q", name, parameter.Type)
	}
}

func parameterError(name, raw string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("invalid value %q for --%s: %w", raw, name, err)
}
