package main

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/rs/zerolog/log"
)

// generateTypeScriptTypings generates complete TypeScript definitions
func generateTypeScriptTypings(schema *APISchema, searchPath string) string {
	// Create template functions
	funcMap := template.FuncMap{
		"cleanTypeName":      cleanTypeName,
		"goToTypeScriptType": goToTypeScriptType,
		"getAllStructs":      func() []APIType { return getAllReferencedStructs(schema) },
		"getSortedHandlers":  func() []APIHandler { return getSortedHandlers(schema) },
		"getSortedMethods":   func() []string { return getSortedMethods(schema) },
		"getReturnType":      getReturnType,
		"getParameterList":   getParameterList,
		"hasParameters":      hasParameters,
		"getStringAliasInfo": func() []StringAliasInfo { return getStringAliasInfoWithReflection(searchPath) },
		"sub":                func(a, b int) int { return a - b },
		"pad":                func(s string, width int) string { return padString(s, width) },
		"padComment":         func(fieldName, fieldType string) string { return padComment(fieldName, fieldType) },
		"isOptionalType":     func(goType string) bool { return isOptionalType(goType) },
	}

	// Parse the main template
	tmpl, err := template.New("typescript").Funcs(funcMap).Parse(typescriptTemplate)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse TypeScript template")
	}

	// Execute template
	var output strings.Builder
	err = tmpl.Execute(&output, schema)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to execute TypeScript template")
	}

	return output.String()
}

// padString pads a string to the specified width
func padString(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// padComment calculates the proper padding for field comments
func padComment(fieldName, fieldType string) string {
	// Calculate the length of the field declaration part
	// Format: "  fieldName: fieldType;"
	declarationLength := 2 + len(fieldName) + 2 + len(fieldType) + 1 // "  " + fieldName + ": " + fieldType + ";"

	// Target alignment at column 40
	targetColumn := 40
	if declarationLength >= targetColumn {
		return " " // Just one space if already past target
	}

	return strings.Repeat(" ", targetColumn-declarationLength)
}

// isOptionalType determines if a Go type should be rendered as an optional TypeScript property
func isOptionalType(goType string) bool {
	return goType == "null.String" || goType == "null.Bool" || goType == "null.Int"
}

// cleanTypeName cleans up Go type names for TypeScript
func cleanTypeName(typeName string) string {
	// Remove package prefixes
	if strings.Contains(typeName, ".") {
		parts := strings.Split(typeName, ".")
		return parts[len(parts)-1]
	}
	return typeName
}

// goToTypeScriptType converts Go types to TypeScript types with recursive parsing
func goToTypeScriptType(goType string) string {
	return parseTypeRecursively(goType)
}

// parseTypeRecursively parses Go types recursively to handle complex nested types
func parseTypeRecursively(goType string) string {
	// Remove any leading/trailing whitespace
	goType = strings.TrimSpace(goType)

	// Handle basic types first
	switch goType {
	case "bool":
		return "boolean"
	case "string":
		return "string"
	case "int", "int8", "int16", "int32", "int64":
		return "number"
	case "uint", "uint8", "uint16", "uint32", "uint64":
		return "number"
	case "float32", "float64":
		return "number"
	case "byte":
		return "number"
	case "rune":
		return "string"
	case "error":
		return "string"
	case "usbgadget.ByteSlice":
		return "number[]"
	case "null.String":
		return "string"
	case "null.Bool":
		return "boolean"
	case "null.Int":
		return "number"
	case "interface {}":
		return "any"
	case "time.Duration":
		return "number"
	case "time.Time":
		return "string"
	case "net.IP":
		return "string"
	case "net.IPNet":
		return "string"
	case "net.HardwareAddr":
		return "string"
	}

	// Handle pointer types
	if strings.HasPrefix(goType, "*") {
		innerType := parseTypeRecursively(goType[1:])
		return innerType + " | null"
	}

	// Handle slice types
	if strings.HasPrefix(goType, "[]") {
		elementType := parseTypeRecursively(goType[2:])
		return elementType + "[]"
	}

	// Handle map types with proper bracket matching
	if strings.HasPrefix(goType, "map[") {
		return parseMapType(goType)
	}

	// Handle any remaining interface {} in complex types
	if strings.Contains(goType, "interface {}") {
		return strings.ReplaceAll(goType, "interface {}", "any")
	}

	// Check if this is a string alias (type name != underlying type)
	if isStringAlias(goType) {
		return cleanTypeName(goType)
	}

	// Return cleaned custom type name
	return cleanTypeName(goType)
}

// parseMapType parses map types with proper bracket matching
func parseMapType(goType string) string {
	if !strings.HasPrefix(goType, "map[") {
		return goType
	}

	// Find the key type and value type
	start := 4 // After "map["
	bracketCount := 0
	keyEnd := -1

	// Find the end of the key type by looking for the first ']' at bracket level 0
	for i := start; i < len(goType); i++ {
		char := goType[i]
		if char == '[' {
			bracketCount++
		} else if char == ']' {
			if bracketCount == 0 {
				keyEnd = i
				break
			}
			bracketCount--
		}
	}

	if keyEnd == -1 || keyEnd >= len(goType)-1 {
		return goType // Invalid map type
	}

	keyType := goType[start:keyEnd]
	valueType := goType[keyEnd+1:]

	// Parse key and value types recursively
	tsKeyType := parseTypeRecursively(keyType)
	tsValueType := parseTypeRecursively(valueType)

	return fmt.Sprintf("Record<%s, %s>", tsKeyType, tsValueType)
}

// isStringAlias checks if a type is a string alias
func isStringAlias(typeName string) bool {
	// Known string aliases in the codebase
	stringAliases := map[string]bool{
		"VirtualMediaMode":   true,
		"VirtualMediaSource": true,
	}
	return stringAliases[typeName]
}

// getReturnType returns the TypeScript return type for a handler
func getReturnType(handler APIHandler) string {
	if len(handler.ReturnValues) == 0 {
		return "void"
	} else if len(handler.ReturnValues) == 1 {
		return goToTypeScriptType(handler.ReturnValues[0].Type)
	} else {
		// Multiple return values - use tuple type
		var returnTypes []string
		for _, retVal := range handler.ReturnValues {
			returnTypes = append(returnTypes, goToTypeScriptType(retVal.Type))
		}
		return fmt.Sprintf("[%s]", strings.Join(returnTypes, ", "))
	}
}

// getParameterList returns the TypeScript parameter list for a handler
func getParameterList(handler APIHandler) string {
	if len(handler.Parameters) == 0 {
		return ""
	}

	var paramList []string
	for _, param := range handler.Parameters {
		tsType := goToTypeScriptType(param.Type)
		paramList = append(paramList, fmt.Sprintf("%s: %s", param.Name, tsType))
	}
	return strings.Join(paramList, ", ")
}

// hasParameters returns true if the handler has parameters
func hasParameters(handler APIHandler) bool {
	return len(handler.Parameters) > 0
}

// typescriptTemplate is the main template for generating TypeScript definitions
const typescriptTemplate = `// Code generated by generate_typings.go. DO NOT EDIT.
{{range $struct := getAllStructs}}
{{if eq $struct.Kind "extension"}}
export interface {{cleanTypeName $struct.Name}} extends {{cleanTypeName $struct.Extends}} {
}
{{else}}
export interface {{cleanTypeName $struct.Name}} {
{{range $field := $struct.Fields}}  {{$field.JSONName}}{{if isOptionalType $field.Type}}?{{end}}: {{goToTypeScriptType $field.Type}};{{padComment $field.JSONName (goToTypeScriptType $field.Type)}}// {{$field.Name}} {{$field.Type}}
{{end}}}
{{end}}
{{end}}
// String aliases with constants
{{range $alias := getStringAliasInfo}}
export type {{$alias.Name}} = {{range $i, $const := $alias.Constants}}"{{$const}}"{{if lt $i (sub (len $alias.Constants) 1)}} | {{end}}{{end}};
{{end}}

// JSON-RPC Types
export interface JsonRpcRequest {
  jsonrpc: "2.0";
  method: string;
  params?: unknown;
  id: number | string;
}

export interface JsonRpcError {
  code: number;
  data?: string;
  message: string;
}

export interface JsonRpcSuccessResponse {
  jsonrpc: "2.0";
  result: unknown;
  id: number | string;
}

export interface JsonRpcErrorResponse {
  jsonrpc: "2.0";
  error: JsonRpcError;
  id: number | string;
}

export type JsonRpcResponse = JsonRpcSuccessResponse | JsonRpcErrorResponse;

// RPC Functions
export class JsonRpcClient {
  constructor(private send: (method: string, params: unknown, callback?: (resp: JsonRpcResponse) => void) => void) {}

  private async sendAsync<T>(method: string, params?: unknown): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      this.send(method, params, (response: JsonRpcResponse) => {
        if ('error' in response) {
          reject(new Error('RPC error: ' + response.error.message));
          return;
        }
        resolve(response.result as T);
      });
    });
  }

{{range $handler := getSortedHandlers}}
  async {{$handler.Name}}({{getParameterList $handler}}) {
{{if eq (len $handler.Parameters) 0}}    return this.sendAsync<{{getReturnType $handler}}>('{{$handler.Name}}');
{{else}}    return this.sendAsync<{{getReturnType $handler}}>('{{$handler.Name}}', {
{{range $param := $handler.Parameters}}      {{$param.Name}},
{{end}}    });
{{end}}  }

{{end}}}
`
