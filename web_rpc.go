package kvm

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var httpExposedRPC = map[string]bool{
	"getTLSState":        true,
	"setTLSState":        true,
	"getNetworkSettings": true,
	"setNetworkSettings": true,
	"getDeviceID":        true,
}

func handleRPCDispatch(c *gin.Context) {
	method := c.Param("method")
	if !httpExposedRPC[method] {
		c.JSON(http.StatusNotFound, gin.H{"error": "method not found"})
		return
	}

	params := map[string]any{}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	scopedLogger := jsonRpcLogger.With().Str("method", method).Logger()
	result, err := callRPCHandler(scopedLogger, rpcHandlers[method], params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
