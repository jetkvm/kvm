package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/jetkvm/kvm"
	"github.com/rs/zerolog/log"
)

// NewAPISchema creates a new API schema from the JSON-RPC handlers
func NewAPISchema(searchPath string) *APISchema {
	schema := &APISchema{
		Handlers: make(map[string]APIHandler),
		Types:    make(map[string]APIType),
	}

	handlers := kvm.GetJSONRPCHandlers()
	log.Info().Int("count", len(handlers)).Msg("Processing JSON-RPC handlers")

	for name, handler := range handlers {
		log.Debug().Str("handler", name).Msg("Building API handler")
		apiHandler := buildAPIHandler(name, handler, schema)
		schema.Handlers[name] = apiHandler
	}

	schema.HandlerCount = len(schema.Handlers)
	schema.TypeCount = len(schema.Types)

	log.Info().
		Int("handlers", schema.HandlerCount).
		Int("types", schema.TypeCount).
		Msg("API schema created successfully")

	return schema
}

// buildAPIHandler constructs an APIHandler from a JSON-RPC handler
func buildAPIHandler(name string, handler kvm.JSONRPCHandler, schema *APISchema) APIHandler {
	apiHandler := APIHandler{
		Name:           name,
		FunctionType:   handler.Type.String(),
		ParameterNames: handler.Params,
		Parameters:     make([]APIParameter, 0, handler.Type.NumIn()),
		ReturnValues:   make([]APIReturnValue, 0, handler.Type.NumOut()),
	}

	// Process parameters
	for i := 0; i < handler.Type.NumIn(); i++ {
		paramType := handler.Type.In(i)
		paramName := getParameterName(i, handler.Params)

		apiParam := APIParameter{
			Name: paramName,
			Type: paramType.String(),
		}

		if apiType := extractAPIType(paramType, schema); apiType != nil {
			apiParam.APIType = apiType
			schema.Types[apiType.Name] = *apiType
		}

		apiHandler.Parameters = append(apiHandler.Parameters, apiParam)
	}

	// Process return values
	for i := 0; i < handler.Type.NumOut(); i++ {
		returnType := handler.Type.Out(i)

		apiReturn := APIReturnValue{
			Index: i,
			Type:  returnType.String(),
		}

		if apiType := extractAPIType(returnType, schema); apiType != nil {
			apiReturn.APIType = apiType
			schema.Types[apiType.Name] = *apiType
		}

		apiHandler.ReturnValues = append(apiHandler.ReturnValues, apiReturn)
	}

	return apiHandler
}

// getParameterName safely retrieves a parameter name by index
func getParameterName(index int, paramNames []string) string {
	if index < len(paramNames) {
		return paramNames[index]
	}
	return ""
}

// extractAPIType extracts API type information from a reflect.Type
// It recursively finds and adds nested struct types to the schema
func extractAPIType(t reflect.Type, schema *APISchema) *APIType {
	if t == nil {
		return nil
	}

	switch t.Kind() {
	case reflect.Ptr:
		return extractPointerType(t, schema)
	case reflect.Slice:
		return extractSliceType(t, schema)
	case reflect.Array:
		return extractArrayType(t, schema)
	case reflect.Map:
		return extractMapType(t, schema)
	case reflect.Struct:
		return extractStructType(t, schema)
	case reflect.Interface:
		return extractInterfaceType(t)
	default:
		return extractBasicType(t)
	}
}

// extractPointerType handles pointer types
func extractPointerType(t reflect.Type, schema *APISchema) *APIType {
	elemType := extractAPIType(t.Elem(), schema)
	if elemType == nil {
		return nil
	}

	elemType.IsPointer = true
	elemType.Name = "*" + elemType.Name
	return elemType
}

// extractSliceType handles slice types
func extractSliceType(t reflect.Type, schema *APISchema) *APIType {
	elemType := extractAPIType(t.Elem(), schema)
	if elemType == nil {
		return nil
	}

	elemType.IsSlice = true
	elemType.SliceType = elemType.Name
	elemType.Name = "[]" + elemType.Name
	return elemType
}

// extractArrayType handles array types
func extractArrayType(t reflect.Type, schema *APISchema) *APIType {
	elemType := extractAPIType(t.Elem(), schema)
	if elemType == nil {
		return nil
	}

	elemType.Name = fmt.Sprintf("[%d]%s", t.Len(), elemType.Name)
	return elemType
}

// extractMapType handles map types
func extractMapType(t reflect.Type, schema *APISchema) *APIType {
	keyType := extractAPIType(t.Key(), schema)
	valueType := extractAPIType(t.Elem(), schema)

	if keyType == nil || valueType == nil {
		return nil
	}

	return &APIType{
		Name:         fmt.Sprintf("map[%s]%s", keyType.Name, valueType.Name),
		Kind:         TypeKindMap,
		IsMap:        true,
		MapKeyType:   keyType.Name,
		MapValueType: valueType.Name,
	}
}

// extractStructType handles struct types
func extractStructType(t reflect.Type, schema *APISchema) *APIType {
	// Skip null.* structs as they are handled as optional properties
	if strings.HasPrefix(t.String(), "null.") {
		return nil
	}

	apiType := &APIType{
		Name:   t.String(),
		Kind:   TypeKindStruct,
		Fields: make([]APIField, 0, t.NumField()),
	}

	// Extract package name
	if pkgPath := t.PkgPath(); pkgPath != "" {
		apiType.Package = extractPackageName(pkgPath)
	}

	// Extract fields
	embeddedStructs := make([]string, 0)
	regularFields := make([]APIField, 0)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check if this is an embedded struct (anonymous field)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			embeddedStructs = append(embeddedStructs, field.Type.String())
			// Recursively extract nested struct types from embedded structs
			if nestedType := extractAPIType(field.Type, schema); nestedType != nil {
				if nestedType.Kind == TypeKindStruct {
					schema.Types[nestedType.Name] = *nestedType
				}
			}
		} else {
			apiField := buildAPIField(field)
			regularFields = append(regularFields, apiField)

			// Recursively extract nested struct types from field types
			if nestedType := extractAPIType(field.Type, schema); nestedType != nil {
				if nestedType.Kind == TypeKindStruct {
					schema.Types[nestedType.Name] = *nestedType
				}
			}
		}
	}

	// If we have exactly one embedded struct and no regular fields, mark it as an extension
	if len(embeddedStructs) == 1 && len(regularFields) == 0 {
		apiType.Kind = TypeKindExtension
		apiType.Extends = embeddedStructs[0]
	} else {
		apiType.Fields = regularFields
	}

	return apiType
}

// extractInterfaceType handles interface types
func extractInterfaceType(t reflect.Type) *APIType {
	return &APIType{
		Name: t.String(),
		Kind: TypeKindInterface,
	}
}

// extractBasicType handles basic Go types
func extractBasicType(t reflect.Type) *APIType {
	if isBasicType(t.String()) {
		return &APIType{
			Name: t.String(),
			Kind: TypeKindBasic,
		}
	}
	return nil
}

// buildAPIField constructs an APIField from a reflect.StructField
func buildAPIField(field reflect.StructField) APIField {
	apiField := APIField{
		Name:       field.Name,
		JSONName:   field.Name, // Default to field name
		Type:       field.Type.String(),
		IsExported: field.IsExported(),
		Tag:        string(field.Tag),
	}

	// Parse JSON tag
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		if jsonName := parseJSONTag(jsonTag); jsonName != "" {
			apiField.JSONName = jsonName
		}
	}

	return apiField
}

// parseJSONTag extracts the JSON field name from a JSON tag
func parseJSONTag(jsonTag string) string {
	parts := strings.Split(jsonTag, ",")
	if len(parts) > 0 && parts[0] != "" && parts[0] != "-" {
		return parts[0]
	}
	return ""
}

// extractPackageName extracts the package name from a package path
func extractPackageName(pkgPath string) string {
	parts := strings.Split(pkgPath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// GetStructTypes returns all struct types from the schema
func (s *APISchema) GetStructTypes() []APIType {
	structs := make([]APIType, 0)
	for _, apiType := range s.Types {
		if apiType.Kind == TypeKindStruct {
			structs = append(structs, apiType)
		}
	}
	return structs
}

// getSortedHandlers returns handlers sorted by name
func getSortedHandlers(schema *APISchema) []APIHandler {
	var handlers []APIHandler
	for _, handler := range schema.Handlers {
		handlers = append(handlers, handler)
	}
	sort.Slice(handlers, func(i, j int) bool {
		return handlers[i].Name < handlers[j].Name
	})
	return handlers
}

// getSortedMethods returns method names sorted alphabetically
func getSortedMethods(schema *APISchema) []string {
	var methods []string
	for name := range schema.Handlers {
		methods = append(methods, name)
	}
	sort.Strings(methods)
	return methods
}

// getAllReferencedStructs recursively finds all structs referenced in the API
func getAllReferencedStructs(schema *APISchema) []APIType {
	// Start with all structs found in handlers
	allStructs := make(map[string]APIType)

	// Add all structs from handlers
	for _, apiType := range schema.GetStructTypes() {
		allStructs[apiType.Name] = apiType
	}

	// Also add all structs from the complete schema that might be referenced
	for name, apiType := range schema.Types {
		if apiType.Kind == TypeKindStruct || apiType.Kind == TypeKindExtension {
			allStructs[name] = apiType
		}
	}

	// Recursively find all referenced structs
	changed := true
	for changed {
		changed = false
		for _, apiType := range allStructs {
			referencedStructs := findReferencedStructs(apiType, schema)
			for _, refStruct := range referencedStructs {
				if _, exists := allStructs[refStruct.Name]; !exists {
					allStructs[refStruct.Name] = refStruct
					changed = true
				}
			}
		}
	}

	// Convert map to slice
	var result []APIType
	for _, apiType := range allStructs {
		result = append(result, apiType)
	}

	return result
}

// findReferencedStructs finds structs referenced in a given API type
func findReferencedStructs(apiType APIType, schema *APISchema) []APIType {
	var referenced []APIType

	for _, field := range apiType.Fields {
		if isStructType(field.Type) {
			structName := extractStructName(field.Type)
			if structType, exists := schema.Types[structName]; exists {
				referenced = append(referenced, structType)
			}
		}
	}

	return referenced
}

// isStructType checks if a type string represents a struct
func isStructType(typeStr string) bool {
	// Check if it's a custom type (not basic Go types)
	return !isBasicType(typeStr) &&
		!strings.HasPrefix(typeStr, "[]") &&
		!strings.HasPrefix(typeStr, "map[") &&
		!strings.HasPrefix(typeStr, "*") &&
		!strings.HasPrefix(typeStr, "[")
}

// extractStructName extracts the struct name from a type string
func extractStructName(typeStr string) string {
	// Remove array/slice prefixes
	if strings.HasPrefix(typeStr, "[]") {
		return typeStr[2:]
	}
	if strings.HasPrefix(typeStr, "*") {
		return typeStr[1:]
	}
	return typeStr
}

// isBasicType checks if a type is a basic Go type
func isBasicType(typeName string) bool {
	basicTypes := map[string]bool{
		"bool":       true,
		"string":     true,
		"int":        true,
		"int8":       true,
		"int16":      true,
		"int32":      true,
		"int64":      true,
		"uint":       true,
		"uint8":      true,
		"uint16":     true,
		"uint32":     true,
		"uint64":     true,
		"uintptr":    true,
		"float32":    true,
		"float64":    true,
		"complex64":  true,
		"complex128": true,
		"byte":       true,
		"rune":       true,
		"error":      true,
	}

	return basicTypes[typeName]
}
