package kvm

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// apiRPCMaxBody bounds a JSON-RPC request body; the largest legitimate
// requests (keyboard macros, EDID blobs) are a few kilobytes.
const apiRPCMaxBody = 64 * 1024

// handleAPIRPC serves the JSON-RPC methods over HTTP for scripts and
// automation that have no WebRTC session. One request per call, dispatched
// through the same handler table as the data channel; handlers run without a
// session, which the ones that need one already report as an error.
func handleAPIRPC(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, apiRPCMaxBody+1))
	if err != nil || len(body) > apiRPCMaxBody {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
		return
	}

	var request JSONRPCRequest
	if err := json.Unmarshal(body, &request); err != nil || request.Method == "" {
		c.JSON(http.StatusBadRequest, JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   map[string]any{"code": -32700, "message": "Parse error"},
			ID:      request.ID,
		})
		return
	}

	handler, ok := rpcHandlers[request.Method]
	if !ok {
		c.JSON(http.StatusNotFound, JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   map[string]any{"code": -32601, "message": "Method not found"},
			ID:      request.ID,
		})
		return
	}

	scopedLogger := jsonRpcLogger.With().Str("method", request.Method).Str("transport", "http").Logger()
	result, err := callRPCHandler(scopedLogger, handler, request.Params, nil)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("RPC handler failed")
		c.JSON(http.StatusOK, JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   map[string]any{"code": -32603, "message": "Internal error", "data": err.Error()},
			ID:      request.ID,
		})
		return
	}

	c.JSON(http.StatusOK, JSONRPCResponse{JSONRPC: "2.0", Result: result, ID: request.ID})
}
