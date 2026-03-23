package handler

import (
	"fmt"
	"net/http"

	"github.com/5g-lmf/sbi-gateway/internal/api"
	"github.com/5g-lmf/sbi-gateway/internal/grpcclient"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SubscriptionHandler handles Nllmf subscription requests
type SubscriptionHandler struct {
	clients *grpcclient.Clients
	logger  *zap.Logger
}

// NewSubscriptionHandler creates a new subscription handler
func NewSubscriptionHandler(clients *grpcclient.Clients, logger *zap.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{clients: clients, logger: logger}
}

// Subscribe handles POST /nlmf-loc/v1/subscriptions
func (h *SubscriptionHandler) Subscribe(c *gin.Context) {
	var req api.SubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	// Validate required fields
	if req.Supi == "" {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "supi is required for subscription",
		})
		return
	}
	if req.NotifUri == "" {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "notifUri is required",
		})
		return
	}
	if req.EventType == "" {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "eventType is required",
		})
		return
	}

	h.logger.Info("subscription request received",
		zap.String("supi", req.Supi),
		zap.String("eventType", req.EventType),
		zap.String("notifUri", req.NotifUri),
	)

	subID, err := h.clients.Subscribe(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error("subscription failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, api.ProblemDetails{
			Title:  "Internal Server Error",
			Status: http.StatusInternalServerError,
			Detail: err.Error(),
		})
		return
	}

	c.Header("Location", fmt.Sprintf("/nlmf-loc/v1/subscriptions/%s", subID))
	c.JSON(http.StatusCreated, api.SubscriptionResponse{SubscriptionId: subID})
}

// Unsubscribe handles DELETE /nlmf-loc/v1/subscriptions/{subscriptionId}
func (h *SubscriptionHandler) Unsubscribe(c *gin.Context) {
	subID := c.Param("subscriptionId")
	if subID == "" {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "subscriptionId path parameter is required",
		})
		return
	}

	h.logger.Info("unsubscribe request", zap.String("subscriptionId", subID))

	if err := h.clients.Unsubscribe(c.Request.Context(), subID); err != nil {
		h.logger.Error("unsubscribe failed", zap.String("subscriptionId", subID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, api.ProblemDetails{
			Title:  "Internal Server Error",
			Status: http.StatusInternalServerError,
			Detail: err.Error(),
		})
		return
	}

	c.Status(http.StatusNoContent)
}
