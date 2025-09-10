package main

// TypeKind represents the kind of API type
type TypeKind string

const (
	// TypeKindStruct represents a struct type
	TypeKindStruct TypeKind = "struct"
	// TypeKindInterface represents an interface type
	TypeKindInterface TypeKind = "interface"
	// TypeKindBasic represents a basic Go type
	TypeKindBasic TypeKind = "basic"
	// TypeKindMap represents a map type
	TypeKindMap TypeKind = "map"
	// TypeKindSlice represents a slice type
	TypeKindSlice TypeKind = "slice"
	// TypeKindArray represents an array type
	TypeKindArray TypeKind = "array"
	// TypeKindPointer represents a pointer type
	TypeKindPointer TypeKind = "pointer"
)

// APIType represents a type used in the JSON-RPC API
type APIType struct {
	Name         string     `json:"name"`
	Package      string     `json:"package"`
	Kind         TypeKind   `json:"kind"`
	Fields       []APIField `json:"fields,omitempty"`
	IsPointer    bool       `json:"is_pointer"`
	IsSlice      bool       `json:"is_slice"`
	IsMap        bool       `json:"is_map"`
	MapKeyType   string     `json:"map_key_type,omitempty"`
	MapValueType string     `json:"map_value_type,omitempty"`
	SliceType    string     `json:"slice_type,omitempty"`
}

// APIField represents a field in a struct
type APIField struct {
	Name       string `json:"name"`
	JSONName   string `json:"json_name"`
	Type       string `json:"type"`
	IsExported bool   `json:"is_exported"`
	Tag        string `json:"tag"`
}

// APIParameter represents a parameter in a JSON-RPC handler
type APIParameter struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	APIType *APIType `json:"api_type,omitempty"`
}

// APIReturnValue represents a return value from a JSON-RPC handler
type APIReturnValue struct {
	Index   int      `json:"index"`
	Type    string   `json:"type"`
	APIType *APIType `json:"api_type,omitempty"`
}

// APIHandler represents a complete JSON-RPC handler
type APIHandler struct {
	Name           string           `json:"name"`
	FunctionType   string           `json:"function_type"`
	Parameters     []APIParameter   `json:"parameters"`
	ReturnValues   []APIReturnValue `json:"return_values"`
	ParameterNames []string         `json:"parameter_names"`
}

// APISchema represents the complete JSON-RPC API schema
type APISchema struct {
	Handlers     map[string]APIHandler `json:"handlers"`
	Types        map[string]APIType    `json:"types"`
	TypeCount    int                   `json:"type_count"`
	HandlerCount int                   `json:"handler_count"`
}

// StringAliasInfo represents information about a string alias and its constants
type StringAliasInfo struct {
	Name      string
	Constants []string
}
