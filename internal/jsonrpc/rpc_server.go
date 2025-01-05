package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type JSONRPCServer struct {
	writer io.Writer

	handlers map[string]*RPCHandler
	nextId   atomic.Int64

	responseChannelsMutex sync.Mutex
	responseChannels      map[int64]chan JSONRPCResponse
}

func NewJSONRPCServer(writer io.Writer, handlers map[string]*RPCHandler) *JSONRPCServer {
	return &JSONRPCServer{
		writer:           writer,
		handlers:         handlers,
		responseChannels: make(map[int64]chan JSONRPCResponse),
		nextId:           atomic.Int64{},
	}
}

func (s *JSONRPCServer) Request(method string, params map[string]interface{}, result interface{}) *JSONRPCResponseError {
	id := s.nextId.Add(1)
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return &JSONRPCResponseError{
			Code:    -32700,
			Message: "Parse error",
			Data:    err,
		}
	}

	// log.Printf("Sending RPC request: Method=%s, Params=%v, ID=%d", method, params, id)

	responseChan := make(chan JSONRPCResponse, 1)
	s.responseChannelsMutex.Lock()
	s.responseChannels[id] = responseChan
	s.responseChannelsMutex.Unlock()
	defer func() {
		s.responseChannelsMutex.Lock()
		delete(s.responseChannels, id)
		s.responseChannelsMutex.Unlock()
	}()

	_, err = s.writer.Write(requestBytes)
	if err != nil {
		return &JSONRPCResponseError{
			Code:    -32603,
			Message: "Internal error",
			Data:    err,
		}
	}

	timeout := time.After(5 * time.Second)
	select {
	case response := <-responseChan:
		if response.Error != nil {
			return response.Error
		}

		rawResult, err := json.Marshal(response.Result)
		if err != nil {
			return &JSONRPCResponseError{
				Code:    -32603,
				Message: "Internal error",
				Data:    err,
			}
		}

		if err := json.Unmarshal(rawResult, result); err != nil {
			return &JSONRPCResponseError{
				Code:    -32603,
				Message: "Internal error",
				Data:    err,
			}
		}

		return nil
	case <-timeout:
		return &JSONRPCResponseError{
			Code:    -32603,
			Message: "Internal error",
			Data:    "timeout waiting for response",
		}
	}
}

type JSONRPCMessage struct {
	Method *string `json:"method,omitempty"`
	ID     *int64  `json:"id,omitempty"`
}

func (s *JSONRPCServer) HandleMessage(data []byte) error {
	// Data will either be a JSONRPCRequest or JSONRPCResponse object
	// We need to determine which one it is
	var raw JSONRPCMessage
	err := json.Unmarshal(data, &raw)
	if err != nil {
		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCResponseError{
				Code:    -32700,
				Message: "Parse error",
			},
			ID: 0,
		}
		return s.writeResponse(errorResponse)
	}

	if raw.Method == nil && raw.ID != nil {
		var resp JSONRPCResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			fmt.Println("error unmarshalling response", err)
			return err
		}

		s.responseChannelsMutex.Lock()
		responseChan, ok := s.responseChannels[*raw.ID]
		s.responseChannelsMutex.Unlock()
		if ok {
			responseChan <- resp
		} else {
			log.Println("No response channel found for ID", resp.ID)
		}
		return nil
	}

	var request JSONRPCRequest
	err = json.Unmarshal(data, &request)
	if err != nil {
		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCResponseError{
				Code:    -32700,
				Message: "Parse error",
			},
			ID: 0,
		}
		return s.writeResponse(errorResponse)
	}

	//log.Printf("Received RPC request: Method=%s, Params=%v, ID=%d", request.Method, request.Params, request.ID)
	handler, ok := s.handlers[request.Method]
	if !ok {
		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCResponseError{
				Code:    -32601,
				Message: "Method not found",
			},
			ID: request.ID,
		}
		return s.writeResponse(errorResponse)
	}

	result, err := callRPCHandler(handler, request.Params)
	if err != nil {
		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCResponseError{
				Code:    -32603,
				Message: "Internal error",
				Data:    err.Error(),
			},
			ID: request.ID,
		}
		return s.writeResponse(errorResponse)
	}

	response := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      request.ID,
	}
	return s.writeResponse(response)
}

func (s *JSONRPCServer) writeResponse(response JSONRPCResponse) error {
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = s.writer.Write(responseBytes)
	return err
}

func callRPCHandler(handler *RPCHandler, params map[string]interface{}) (interface{}, error) {
	handlerValue := reflect.ValueOf(handler.Func)
	handlerType := handlerValue.Type()

	if handlerType.Kind() != reflect.Func {
		return nil, errors.New("handler is not a function")
	}

	numParams := handlerType.NumIn()
	args := make([]reflect.Value, numParams)
	// Get the parameter names from the RPCHandler
	paramNames := handler.Params

	if len(paramNames) != numParams {
		return nil, errors.New("mismatch between handler parameters and defined parameter names")
	}

	for i := 0; i < numParams; i++ {
		paramType := handlerType.In(i)
		paramName := paramNames[i]
		paramValue, ok := params[paramName]
		if !ok {
			return nil, errors.New("missing parameter: " + paramName)
		}

		convertedValue := reflect.ValueOf(paramValue)
		if !convertedValue.Type().ConvertibleTo(paramType) {
			if paramType.Kind() == reflect.Slice && (convertedValue.Kind() == reflect.Slice || convertedValue.Kind() == reflect.Array) {
				newSlice := reflect.MakeSlice(paramType, convertedValue.Len(), convertedValue.Len())
				for j := 0; j < convertedValue.Len(); j++ {
					elemValue := convertedValue.Index(j)
					if elemValue.Kind() == reflect.Interface {
						elemValue = elemValue.Elem()
					}
					if !elemValue.Type().ConvertibleTo(paramType.Elem()) {
						// Handle float64 to uint8 conversion
						if elemValue.Kind() == reflect.Float64 && paramType.Elem().Kind() == reflect.Uint8 {
							intValue := int(elemValue.Float())
							if intValue < 0 || intValue > 255 {
								return nil, fmt.Errorf("value out of range for uint8: %v", intValue)
							}
							newSlice.Index(j).SetUint(uint64(intValue))
						} else {
							fromType := elemValue.Type()
							toType := paramType.Elem()
							return nil, fmt.Errorf("invalid element type in slice for parameter %s: from %v to %v", paramName, fromType, toType)
						}
					} else {
						newSlice.Index(j).Set(elemValue.Convert(paramType.Elem()))
					}
				}
				args[i] = newSlice
			} else if paramType.Kind() == reflect.Struct && convertedValue.Kind() == reflect.Map {
				jsonData, err := json.Marshal(convertedValue.Interface())
				if err != nil {
					return nil, fmt.Errorf("failed to marshal map to JSON: %v", err)
				}

				newStruct := reflect.New(paramType).Interface()
				if err := json.Unmarshal(jsonData, newStruct); err != nil {
					return nil, fmt.Errorf("failed to unmarshal JSON into struct: %v", err)
				}
				args[i] = reflect.ValueOf(newStruct).Elem()
			} else {
				return nil, fmt.Errorf("invalid parameter type for: %s", paramName)
			}
		} else {
			args[i] = convertedValue.Convert(paramType)
		}
	}

	results := handlerValue.Call(args)

	if len(results) == 0 {
		return nil, nil
	}

	if len(results) == 1 {
		if results[0].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !results[0].IsNil() {
				return nil, results[0].Interface().(error)
			}
			return nil, nil
		}
		return results[0].Interface(), nil
	}

	if len(results) == 2 && results[1].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if !results[1].IsNil() {
			return nil, results[1].Interface().(error)
		}
		return results[0].Interface(), nil
	}

	return nil, errors.New("unexpected return values from handler")
}
