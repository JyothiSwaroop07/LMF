// Package server implements the gRPC PrivacyAuthService.
package server

import (
	"context"
	"fmt"

	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/privacy-auth/internal/audit"
	"github.com/5g-lmf/privacy-auth/internal/auth"
	"github.com/5g-lmf/privacy-auth/internal/privacy"
	"go.uber.org/zap"
)

// PrivacyAuthServer implements the PrivacyAuthService gRPC interface.
type PrivacyAuthServer struct {
	checker   *privacy.PrivacyChecker
	validator *auth.TokenValidator
	auditor   *audit.AuditStore
	logger    *zap.Logger
}

// NewPrivacyAuthServer creates a PrivacyAuthServer.
func NewPrivacyAuthServer(
	checker *privacy.PrivacyChecker,
	validator *auth.TokenValidator,
	auditor *audit.AuditStore,
	logger *zap.Logger,
) *PrivacyAuthServer {
	return &PrivacyAuthServer{
		checker:   checker,
		validator: validator,
		auditor:   auditor,
		logger:    logger,
	}
}

// CheckPrivacy validates a token and enforces LCS privacy policy.
func (s *PrivacyAuthServer) CheckPrivacy(ctx context.Context, token string, req privacy.PrivacyCheckRequest) (*privacy.PrivacyCheckResponse, error) {
	// 1. Validate bearer token
	claims, err := s.validator.Validate(ctx, token)
	if err != nil {
		middleware.PrivacyChecksTotal.WithLabelValues("denied").Inc()
		return &privacy.PrivacyCheckResponse{
			Allowed:      false,
			DenialReason: fmt.Sprintf("invalid token: %v", err),
		}, nil
	}

	// Require nlmf-loc scope
	if !s.validator.HasScope(claims, "nlmf-loc") {
		middleware.PrivacyChecksTotal.WithLabelValues("denied").Inc()
		return &privacy.PrivacyCheckResponse{
			Allowed:      false,
			DenialReason: "token missing nlmf-loc scope",
		}, nil
	}

	// 2. Enforce privacy policy
	resp, err := s.checker.CheckPrivacy(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("privacy check: %w", err)
	}

	// 3. Write audit record
	outcome := "ALLOWED"
	if !resp.Allowed {
		outcome = "DENIED"
	}
	s.auditor.Write(audit.AuditRecord{
		Supi:          req.Supi,
		SessionID:     req.SessionID,
		LcsClientType: string(req.LcsClientType),
		PrivacyClass:  string(resp.PrivacyClass),
		Outcome:       outcome,
		DenialReason:  resp.DenialReason,
	})

	s.logger.Info("privacy check complete",
		zap.String("supi", req.Supi),
		zap.String("outcome", outcome),
		zap.String("privacyClass", string(resp.PrivacyClass)),
	)

	return resp, nil
}

// ValidateToken validates a bearer token and returns its scope.
func (s *PrivacyAuthServer) ValidateToken(ctx context.Context, rawToken string) (string, error) {
	claims, err := s.validator.Validate(ctx, rawToken)
	if err != nil {
		return "", fmt.Errorf("token invalid: %w", err)
	}
	return claims.Scope, nil
}
