package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/5g-lmf/sbi-gateway/internal/api"
	"github.com/5g-lmf/sbi-gateway/internal/grpcclient"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// LocationHandler handles Nllmf location requests
type LocationHandler struct {
	clients *grpcclient.Clients
	logger  *zap.Logger
}

// NewLocationHandler creates a new location handler
func NewLocationHandler(clients *grpcclient.Clients, logger *zap.Logger) *LocationHandler {
	return &LocationHandler{clients: clients, logger: logger}
}

// DetermineLocation handles POST /nlmf-loc/v1/location-contexts
func (h *LocationHandler) DetermineLocation(c *gin.Context) {

	var req api.LocationContextData
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	// Validate: at least one UE identifier required
	if req.Supi == "" && req.Pei == "" && req.Gpsi == "" {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "at least one of supi, pei, or gpsi must be provided",
		})
		return
	}

	// Validate lcsQoS
	if req.LcsQoS.ResponseTime == "" {
		req.LcsQoS.ResponseTime = "DELAY_TOLERANT"
	}
	if req.LcsQoS.Accuracy == 0 {
		req.LcsQoS.Accuracy = 50
	}
	if req.LcsQoS.ConfidenceLevel == 0 {
		req.LcsQoS.ConfidenceLevel = 95
	}

	sessionID := uuid.New().String()

	h.logger.Info("location request received at sbi-gateway:location-handler",
		zap.String("sessionId", sessionID),
		zap.String("supi", req.Supi),
		zap.String("clientType", req.LcsClientType),
		zap.String("responseTime", req.LcsQoS.ResponseTime),
		zap.Int("accuracy", req.LcsQoS.Accuracy),
	)

	// Forward to location-request gRPC service
	result, err := h.clients.DetermineLocation(c.Request.Context(), sessionID, &req)
	if err != nil {
		h.logger.Error("location determination failed",
			zap.String("sessionId", sessionID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, api.ProblemDetails{
			Title:  "Internal Server Error",
			Status: http.StatusInternalServerError,
			Detail: err.Error(),
		})
		return
	}

	// Set Location header with session reference
	c.Header("Location", fmt.Sprintf("/nlmf-loc/v1/location-contexts/%s", sessionID))
	c.JSON(http.StatusOK, result)
}

// CancelLocation handles DELETE /nlmf-loc/v1/location-contexts/{lcsSessionRef}
func (h *LocationHandler) CancelLocation(c *gin.Context) {
	sessionRef := c.Param("lcsSessionRef")
	if sessionRef == "" {
		c.JSON(http.StatusBadRequest, api.ProblemDetails{
			Title:  "Bad Request",
			Status: http.StatusBadRequest,
			Detail: "lcsSessionRef path parameter is required",
		})
		return
	}

	h.logger.Info("cancel location request", zap.String("sessionRef", sessionRef))

	if err := h.clients.CancelLocation(c.Request.Context(), sessionRef); err != nil {
		h.logger.Error("cancel location failed",
			zap.String("sessionRef", sessionRef),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, api.ProblemDetails{
			Title:  "Internal Server Error",
			Status: http.StatusInternalServerError,
			Detail: err.Error(),
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// buildResponse converts internal gRPC result to Nllmf JSON response
func buildResponse(sessionID string, lat, lon, alt, uncertainty float64, method, accuracy string) *api.LocationContextDataResp {
	return &api.LocationContextDataResp{
		LocationEstimate: api.LocationEstimateJson{
			Shape:       "POINT_ALTITUDE_UNCERTAINTY",
			Point:       &api.LatLon{Lat: lat, Lon: lon},
			Altitude:    alt,
			Uncertainty: uncertainty,
			Confidence:  95,
		},
		AccuracyFulfilmentIndicator: accuracy,
		PositioningDataList: []api.PositioningDataEntry{
			{PosMethod: method, PosUsage: "POSITION_USED"},
		},
		Timestamp: time.Now().UTC(),
	}
}
