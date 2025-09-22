package pipeline

import (
	"fmt"
	"strings"
)

// Operation represents a single pipeline operation
type Operation struct {
	Name   string
	Args   []string
	Result interface{}
}

// Pipeline represents a series of operations to perform on downloaded packages
type Pipeline struct {
	Expression string
	Operations []Operation
	WorkDir    string
	Debug      bool
}

// ParsePipeline parses a pipeline expression into operations
func ParsePipeline(expr string) (*Pipeline, error) {
	if expr == "" {
		return nil, nil
	}

	p := &Pipeline{
		Expression: expr,
		Operations: []Operation{},
	}

	// Clean up the expression
	expr = strings.TrimSpace(expr)
	expr = strings.ReplaceAll(expr, "\n", " ")
	expr = strings.ReplaceAll(expr, "\t", " ")

	// Split by && to get individual operations
	parts := splitByOperator(expr, "&&")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		op, err := parseOperation(part)
		if err != nil {
			return nil, fmt.Errorf("failed to parse operation '%s': %w", part, err)
		}
		p.Operations = append(p.Operations, op)
	}

	return p, nil
}

// parseOperation parses a single operation from string
func parseOperation(opStr string) (Operation, error) {
	opStr = strings.TrimSpace(opStr)

	// Handle function calls like unarchive(glob("*.txz"))
	if idx := strings.Index(opStr, "("); idx > 0 {
		name := strings.TrimSpace(opStr[:idx])
		argsStr := opStr[idx+1:]

		// Find the matching closing parenthesis
		closeIdx := findMatchingParen(argsStr)
		if closeIdx < 0 {
			return Operation{}, fmt.Errorf("unmatched parentheses in operation: %s", opStr)
		}

		argsStr = argsStr[:closeIdx]
		args := parseArguments(argsStr)

		return Operation{
			Name: name,
			Args: args,
		}, nil
	}

	// Simple operation without arguments
	return Operation{
		Name: opStr,
		Args: []string{},
	}, nil
}

// parseArguments parses function arguments, handling nested function calls
func parseArguments(argsStr string) []string {
	args := []string{}
	current := strings.Builder{}
	parenDepth := 0
	inQuote := false
	quoteChar := rune(0)

	for _, ch := range argsStr {
		switch ch {
		case '"', '\'':
			if !inQuote {
				inQuote = true
				quoteChar = ch
				current.WriteRune(ch)
			} else if ch == quoteChar {
				inQuote = false
				quoteChar = 0
				current.WriteRune(ch)
			} else {
				current.WriteRune(ch)
			}
		case '(':
			if !inQuote {
				parenDepth++
			}
			current.WriteRune(ch)
		case ')':
			if !inQuote {
				parenDepth--
			}
			current.WriteRune(ch)
		case ',':
			if !inQuote && parenDepth == 0 {
				if arg := strings.TrimSpace(current.String()); arg != "" {
					args = append(args, arg)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Add the last argument
	if arg := strings.TrimSpace(current.String()); arg != "" {
		args = append(args, arg)
	}

	// Strip quotes from arguments if they're not function calls
	for i, arg := range args {
		if !strings.Contains(arg, "(") {
			arg = strings.TrimSpace(arg)
			if (strings.HasPrefix(arg, "\"") && strings.HasSuffix(arg, "\"")) ||
				(strings.HasPrefix(arg, "'") && strings.HasSuffix(arg, "'")) {
				args[i] = arg[1 : len(arg)-1]
			}
		}
	}

	return args
}

// splitByOperator splits a string by an operator, respecting parentheses and quotes
func splitByOperator(str string, op string) []string {
	parts := []string{}
	current := strings.Builder{}
	parenDepth := 0
	inQuote := false
	quoteChar := rune(0)

	runes := []rune(str)
	opRunes := []rune(op)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		// Handle quotes
		if ch == '"' || ch == '\'' {
			if !inQuote {
				inQuote = true
				quoteChar = ch
			} else if ch == quoteChar {
				inQuote = false
				quoteChar = 0
			}
			current.WriteRune(ch)
			continue
		}

		// Handle parentheses
		if !inQuote {
			if ch == '(' {
				parenDepth++
			} else if ch == ')' {
				parenDepth--
			}
		}

		// Check for operator
		if !inQuote && parenDepth == 0 {
			if i+len(opRunes) <= len(runes) {
				match := true
				for j, opRune := range opRunes {
					if runes[i+j] != opRune {
						match = false
						break
					}
				}
				if match {
					// Found the operator
					if part := strings.TrimSpace(current.String()); part != "" {
						parts = append(parts, part)
					}
					current.Reset()
					i += len(opRunes) - 1
					continue
				}
			}
		}

		current.WriteRune(ch)
	}

	// Add the last part
	if part := strings.TrimSpace(current.String()); part != "" {
		parts = append(parts, part)
	}

	return parts
}

// findMatchingParen finds the index of the matching closing parenthesis
func findMatchingParen(str string) int {
	depth := 1
	inQuote := false
	quoteChar := rune(0)

	for i, ch := range str {
		if ch == '"' || ch == '\'' {
			if !inQuote {
				inQuote = true
				quoteChar = ch
			} else if ch == quoteChar {
				inQuote = false
				quoteChar = 0
			}
		} else if !inQuote {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}

	return -1
}

// OperationType represents the type of pipeline operation
type OperationType string

const (
	OpUnarchive OperationType = "unarchive"
	OpChdir     OperationType = "chdir"
	OpGlob      OperationType = "glob"
	OpCleanup   OperationType = "cleanup"
	OpMove      OperationType = "move"
	OpDelete    OperationType = "delete"
	OpChmod     OperationType = "chmod"
)

// IsValid checks if the operation type is valid
func (o OperationType) IsValid() bool {
	switch o {
	case OpUnarchive, OpChdir, OpGlob, OpCleanup, OpMove, OpDelete, OpChmod:
		return true
	default:
		return false
	}
}