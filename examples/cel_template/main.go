// Package main demonstrates the enhanced template functionality with CEL support
package main

import (
	"fmt"
	"log"

	"github.com/flanksource/deps/pkg/template"
)

func main() {
	fmt.Println("=== flanksource/gomplate Template and CEL Examples ===")

	// Example 1: Traditional Go template
	fmt.Println("1. Traditional Go Template:")
	data := map[string]interface{}{
		"version": "v1.28.0",
		"os":      "linux",
		"arch":    "amd64",
	}

	urlTemplate := "https://storage.googleapis.com/kubernetes-release/release/{{.version}}/bin/{{.os}}/{{.arch}}/kubectl"
	result, err := template.RenderTemplate(urlTemplate, data)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Template: %s\n", urlTemplate)
	fmt.Printf("Result:   %s\n\n", result)

	// Example 2: CEL Expression for version manipulation
	fmt.Println("2. CEL Expression for Version Manipulation:")
	celExpression := "version.startsWith('v') ? version.substring(1) + '_' + os + '_' + arch : version + '_' + os + '_' + arch"
	result, err = template.RenderCELExpression(celExpression, data)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("CEL Expression: %s\n", celExpression)
	fmt.Printf("Result:         %s\n\n", result)

	// Example 3: Complex CEL logic
	fmt.Println("3. Complex CEL Logic:")
	complexData := map[string]interface{}{
		"version":    "v1.2.3-alpha",
		"os":         "darwin",
		"arch":       "arm64",
		"build_type": "release",
	}

	complexCEL := `
		(version.contains('-') ?
			version.split('-')[0].trimPrefix('v') + '_' + version.split('-')[1]
			: version.trimPrefix('v')
		) + '_' + os + '_' + arch +
		(build_type == 'release' ? '' : '_' + build_type)
	`
	result, err = template.RenderCELExpression(complexCEL, complexData)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Complex CEL: %s\n", complexCEL)
	fmt.Printf("Result:      %s\n\n", result)

	// Example 4: Backwards compatibility with existing template functions
	fmt.Println("4. Backwards Compatibility:")
	oldStyleData := map[string]string{
		"version": "1.5.0",
		"os":      "windows",
		"arch":    "amd64",
	}

	terraformTemplate := "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip"
	result, err = template.TemplateString(terraformTemplate, oldStyleData)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Old Style Template: %s\n", terraformTemplate)
	fmt.Printf("Result:             %s\n\n", result)

	// Example 5: CEL with basic string operations
	fmt.Println("5. CEL Basic String Operations:")
	stringData := map[string]interface{}{
		"name":    "MyApp",
		"version": "v2.1.0",
		"env":     "production",
	}

	stringCEL := "name + '-' + version + '-' + env"
	result, err = template.RenderCELExpression(stringCEL, stringData)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("String CEL: %s\n", stringCEL)
	fmt.Printf("Result:     %s\n\n", result)

	fmt.Println("=== All examples completed successfully! ===")
}
