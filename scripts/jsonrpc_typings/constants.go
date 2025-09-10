package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// getStringAliasInfoWithReflection uses reflection to automatically detect constants
// This approach tries to find constants by examining the actual values
func getStringAliasInfoWithReflection(searchPath string) []StringAliasInfo {
	log.Debug().Str("searchPath", searchPath).Msg("Detecting string aliases and constants in single pass")

	// Detect both string aliases and their constants in a single file system walk
	result := detectStringAliasesWithConstants(searchPath)

	// If reflection didn't work, throw an error
	if len(result) == 0 {
		log.Fatal().Msg("No string aliases with constants could be detected. Make sure the types are defined with constants in Go files.")
	}

	log.Debug().Int("detected", len(result)).Msg("String alias detection completed")
	return result
}

// detectStringAliasesWithConstants detects both string aliases and their constants in a single file system walk
func detectStringAliasesWithConstants(searchPath string) []StringAliasInfo {
	var result []StringAliasInfo

	// Walk the specified directory to find Go files
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and our own tool files
		if strings.Contains(path, "_test.go") || strings.Contains(path, "scripts/jsonrpc_typings") {
			return nil
		}

		// Parse the file to find both string aliases and their constants
		aliases := findStringAliasesWithConstantsInFile(path)
		result = append(result, aliases...)

		return nil
	})

	if err != nil {
		log.Fatal().Err(err).Msg("Error walking directory for string alias detection")
	}

	// Remove duplicates based on type name
	uniqueAliases := make([]StringAliasInfo, 0)
	seen := make(map[string]bool)
	for _, alias := range result {
		if !seen[alias.Name] {
			seen[alias.Name] = true
			uniqueAliases = append(uniqueAliases, alias)
		}
	}

	return uniqueAliases
}

// findStringAliasesWithConstantsInFile finds both string aliases and their constants in a single Go file
func findStringAliasesWithConstantsInFile(filePath string) []StringAliasInfo {
	var result []StringAliasInfo

	// Parse the Go file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		log.Debug().Err(err).Str("file", filePath).Msg("Failed to parse file")
		return result
	}

	// First pass: collect all string alias type names
	stringAliases := make(map[string]bool)
	ast.Inspect(node, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			// Check if this is a string alias (type Name string)
			if ident, ok := typeSpec.Type.(*ast.Ident); ok && ident.Name == "string" {
				stringAliases[typeSpec.Name.Name] = true
			}
		}

		return true
	})

	// Second pass: find constants for the string aliases we found
	ast.Inspect(node, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			return true
		}

		// Process each constant specification in the declaration
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			// Check if this constant is typed with one of our string aliases
			if valueSpec.Type == nil {
				continue
			}

			ident, ok := valueSpec.Type.(*ast.Ident)
			if !ok {
				continue
			}
			typeName := ident.Name

			// Check if this type is one of our string aliases
			if _, ok := stringAliases[typeName]; !ok {
				continue
			}

			// Extract string literal values
			for _, value := range valueSpec.Values {
				basicLit, ok := value.(*ast.BasicLit)
				if !ok || basicLit.Kind != token.STRING {
					continue
				}

				// Remove quotes from string literal
				constantValue := strings.Trim(basicLit.Value, "\"")

				// Find or create the StringAliasInfo for this type
				var aliasInfo *StringAliasInfo
				for i := range result {
					if result[i].Name == typeName {
						aliasInfo = &result[i]
						break
					}
				}

				if aliasInfo == nil {
					result = append(result, StringAliasInfo{
						Name:      typeName,
						Constants: []string{},
					})
					aliasInfo = &result[len(result)-1]
				}

				aliasInfo.Constants = append(aliasInfo.Constants, constantValue)
			}
		}

		return true
	})

	return result
}

// batchDetectConstantsForTypes efficiently detects constants for multiple types in a single file system walk
func batchDetectConstantsForTypes(typeNames []string, searchPath string) map[string][]string {
	result := make(map[string][]string)

	// Initialize result map
	for _, typeName := range typeNames {
		result[typeName] = []string{}
	}

	// Walk the specified directory to find Go files
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and our own tool files
		if strings.Contains(path, "_test.go") || strings.Contains(path, "scripts/jsonrpc_typings") {
			return nil
		}

		// Check if this file contains any of the types we're looking for
		fileContainsAnyType := false
		for _, typeName := range typeNames {
			if fileContainsType(path, typeName) {
				fileContainsAnyType = true
				break
			}
		}

		if !fileContainsAnyType {
			return nil
		}

		log.Debug().Str("file", path).Strs("types", typeNames).Msg("Parsing file for constants")

		// Parse constants for all types from this file
		fileConstants := batchParseConstantsFromFile(path, typeNames)

		// Merge results
		for typeName, constants := range fileConstants {
			if len(constants) > 0 {
				result[typeName] = constants
				log.Debug().Str("type", typeName).Strs("constants", constants).Str("file", path).Msg("Found constants")
			}
		}

		return nil
	})

	if err != nil {
		log.Fatal().Err(err).Msg("Error searching for constants")
	}

	return result
}

// batchParseConstantsFromFile parses constants for multiple types from a single Go file
func batchParseConstantsFromFile(filePath string, typeNames []string) map[string][]string {
	result := make(map[string][]string)

	// Initialize result map
	for _, typeName := range typeNames {
		result[typeName] = []string{}
	}

	// Parse the Go file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		log.Debug().Err(err).Str("file", filePath).Msg("Failed to parse file")
		return result
	}

	// Walk the AST to find constant declarations
	ast.Inspect(node, func(n ast.Node) bool {
		// Look for GenDecl nodes (const declarations)
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			return true
		}

		// Process each constant specification in the declaration
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			// Check if this constant is typed with one of our target types
			if valueSpec.Type != nil {
				if ident, ok := valueSpec.Type.(*ast.Ident); ok {
					typeName := ident.Name

					// Check if this type is one we're looking for
					if contains(typeNames, typeName) {
						// Extract string literal values
						for _, value := range valueSpec.Values {
							if basicLit, ok := value.(*ast.BasicLit); ok && basicLit.Kind == token.STRING {
								// Remove quotes from string literal
								constantValue := strings.Trim(basicLit.Value, "\"")
								result[typeName] = append(result[typeName], constantValue)
							}
						}
					}
				}
			}
		}

		return true
	})

	return result
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// fileContainsType checks if a Go file contains a type definition for the given type name
func fileContainsType(filePath, typeName string) bool {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return false
	}

	// Walk the AST to find type definitions
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok == token.TYPE {
				for _, spec := range x.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if typeSpec.Name.Name == typeName {
							found = true
							return false // Stop searching
						}
					}
				}
			}
		}
		return !found // Continue searching if not found yet
	})

	return found
}
