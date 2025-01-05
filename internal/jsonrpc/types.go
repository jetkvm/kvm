package jsonrpc

type JSONRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
	ID      interface{}            `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string                `json:"jsonrpc"`
	Result  interface{}           `json:"result,omitempty"`
	Error   *JSONRPCResponseError `json:"error,omitempty"`
	ID      interface{}           `json:"id"`
}

type JSONRPCResponseError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type JSONRPCEvent struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type RPCHandler struct {
	Func   interface{}
	Params []string
}
