// Package handler - AMF N1N2 callback handler for sbi-gateway.
// Receives POST /namf-comm/callback/ue-contexts/{supi}/n1-n2-messages from AMF
// and delivers the body to the waiting LPP flow in protocol-handler.
package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CallbackDeliverer is implemented by callbackregistry.Registry.
type CallbackDeliverer interface {
	Deliver(supi string, body []byte) bool
}

// CallbackHandler handles AMF N1N2 notification callbacks.
type CallbackHandler struct {
	registry CallbackDeliverer
	logger   *zap.Logger
}

// NewCallbackHandler creates a CallbackHandler.
func NewCallbackHandler(registry CallbackDeliverer, logger *zap.Logger) *CallbackHandler {
	return &CallbackHandler{registry: registry, logger: logger}
}

// HandleN1N2Notification handles:
// POST /namf-comm/callback/ue-contexts/{supi}/n1-n2-messages
func (h *CallbackHandler) HandleN1N2Notification(c *gin.Context) {
	supi := c.Param("supi")

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Error("failed to read AMF callback body",
			zap.String("supi", supi),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read body"})
		return
	}

	h.logger.Info("AMF N1N2 callback received",
		zap.String("supi", supi),
		zap.String("body", string(body)),
		zap.String("contentType", c.Request.Header.Get("Content-Type")),
	)

	delivered := h.registry.Deliver(supi, body)
	if !delivered {
		h.logger.Warn("AMF callback received but no waiter found - nobody waiting for this SUPI",
			zap.String("supi", supi),
		)
	}

	// AMF expects 204 No Content per TS 29.518
	c.Status(http.StatusNoContent)
}
