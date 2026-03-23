// Package privacy implements LCS privacy enforcement per 3GPP TS 23.273 §8.
package privacy

import (
	"context"
	"fmt"
	"time"

	"github.com/5g-lmf/common/types"
	"github.com/5g-lmf/common/middleware"
	"go.uber.org/zap"
)

// PrivacyClass defines privacy restriction levels per TS 23.273
type PrivacyClass string

const (
	PrivacyClassA PrivacyClass = "CLASS_A" // No restriction (emergency)
	PrivacyClassB PrivacyClass = "CLASS_B" // Notify only
	PrivacyClassC PrivacyClass = "CLASS_C" // Notify + verify (UE must allow)
	PrivacyClassD PrivacyClass = "CLASS_D" // Blocked
)

// PrivacyOutcome is the result of a privacy check
type PrivacyOutcome string

const (
	OutcomeAllowed  PrivacyOutcome = "ALLOWED"
	OutcomeDenied   PrivacyOutcome = "DENIED"
	OutcomeNotified PrivacyOutcome = "NOTIFIED"
)

// PrivacyCheckRequest is the input to CheckPrivacy
type PrivacyCheckRequest struct {
	Supi          string
	LcsClientType types.LcsClientType
	SessionID     string
}

// PrivacyCheckResponse is the result of CheckPrivacy
type PrivacyCheckResponse struct {
	Allowed          bool
	PrivacyClass     PrivacyClass
	NotificationSent bool
	DenialReason     string
}

// PrivacyProfile holds a UE's privacy configuration
type PrivacyProfile struct {
	PrivacyClass    PrivacyClass
	AllowedClients  []string
}

// UDMClient fetches privacy profiles from UDM
type UDMClient interface {
	GetPrivacyProfile(ctx context.Context, supi string) (*PrivacyProfile, error)
}

// PrivacyChecker enforces LCS privacy
type PrivacyChecker struct {
	udm            UDMClient
	defaultClass   PrivacyClass
	classC_timeout time.Duration
	logger         *zap.Logger
}

// NewPrivacyChecker creates a new privacy checker
func NewPrivacyChecker(udm UDMClient, defaultClass PrivacyClass, classCTimeoutSeconds int, logger *zap.Logger) *PrivacyChecker {
	return &PrivacyChecker{
		udm:            udm,
		defaultClass:   defaultClass,
		classC_timeout: time.Duration(classCTimeoutSeconds) * time.Second,
		logger:         logger,
	}
}

// CheckPrivacy enforces LCS privacy per TS 23.273 §8
func (p *PrivacyChecker) CheckPrivacy(ctx context.Context, req PrivacyCheckRequest) (*PrivacyCheckResponse, error) {
	logger := p.logger.With(
		zap.String("supi", req.Supi),
		zap.String("sessionId", req.SessionID),
		zap.String("lcsClientType", string(req.LcsClientType)),
	)

	// CLASS_A bypass: emergency services are always allowed
	if req.LcsClientType == types.LcsClientEmergencyServices {
		logger.Info("privacy check bypassed: emergency services")
		middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeAllowed)).Inc()
		return &PrivacyCheckResponse{
			Allowed:      true,
			PrivacyClass: PrivacyClassA,
		}, nil
	}

	// Lawful intercept: allowed but logged
	if req.LcsClientType == types.LcsClientLawfulIntercept {
		logger.Info("privacy check bypassed: lawful intercept")
		middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeAllowed)).Inc()
		return &PrivacyCheckResponse{
			Allowed:      true,
			PrivacyClass: PrivacyClassA,
		}, nil
	}

	// Fetch privacy profile from UDM
	profile, err := p.udm.GetPrivacyProfile(ctx, req.Supi)
	if err != nil {
		logger.Warn("UDM unavailable, applying default privacy class",
			zap.String("default", string(p.defaultClass)),
			zap.Error(err),
		)
		profile = &PrivacyProfile{PrivacyClass: p.defaultClass}
	}

	privClass := profile.PrivacyClass
	logger.Info("privacy profile fetched", zap.String("class", string(privClass)))

	switch privClass {
	case PrivacyClassA:
		// No restriction
		middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeAllowed)).Inc()
		return &PrivacyCheckResponse{
			Allowed:      true,
			PrivacyClass: PrivacyClassA,
		}, nil

	case PrivacyClassB:
		// Notify UE but allow regardless of response
		// (Notification is async; we allow immediately)
		go p.sendNotification(ctx, req.Supi, req.SessionID)
		middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeNotified)).Inc()
		return &PrivacyCheckResponse{
			Allowed:          true,
			PrivacyClass:     PrivacyClassB,
			NotificationSent: true,
		}, nil

	case PrivacyClassC:
		// Notify and require UE consent within timeout
		notified := p.sendNotification(ctx, req.Supi, req.SessionID)
		if !notified {
			// Notification failed → deny
			middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeDenied)).Inc()
			return &PrivacyCheckResponse{
				Allowed:      false,
				PrivacyClass: PrivacyClassC,
				DenialReason: "notification delivery failed",
			}, nil
		}

		// Wait for UE consent (timeout per config)
		allowed := p.waitForConsent(ctx, req.Supi, req.SessionID)
		if !allowed {
			middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeDenied)).Inc()
			return &PrivacyCheckResponse{
				Allowed:          false,
				PrivacyClass:     PrivacyClassC,
				NotificationSent: true,
				DenialReason:     "UE did not consent within timeout",
			}, nil
		}
		middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeAllowed)).Inc()
		return &PrivacyCheckResponse{
			Allowed:          true,
			PrivacyClass:     PrivacyClassC,
			NotificationSent: true,
		}, nil

	case PrivacyClassD:
		logger.Info("privacy check denied: CLASS_D")
		middleware.PrivacyChecksTotal.WithLabelValues(string(OutcomeDenied)).Inc()
		return &PrivacyCheckResponse{
			Allowed:      false,
			PrivacyClass: PrivacyClassD,
			DenialReason: "location services blocked by subscriber profile",
		}, nil

	default:
		return nil, fmt.Errorf("unknown privacy class: %s", privClass)
	}
}

// sendNotification sends a privacy notification to the UE.
// In production, this sends a NAS message via AMF.
// Returns true if notification was sent successfully.
func (p *PrivacyChecker) sendNotification(ctx context.Context, supi, sessionID string) bool {
	p.logger.Info("sending privacy notification to UE",
		zap.String("supi", supi),
		zap.String("sessionId", sessionID),
	)
	// In production: POST to AMF Namf_MT notification endpoint
	return true
}

// waitForConsent waits for UE to respond to the privacy notification.
// Returns true if UE consents, false if timeout or denial.
func (p *PrivacyChecker) waitForConsent(ctx context.Context, supi, sessionID string) bool {
	p.logger.Info("waiting for UE consent",
		zap.String("supi", supi),
		zap.Duration("timeout", p.classC_timeout),
	)
	// In production: listen for UE consent message via AMF callback.
	// Simplified: simulate timeout → deny after deadline.
	select {
	case <-ctx.Done():
		return false
	case <-time.After(p.classC_timeout):
		p.logger.Warn("UE consent timeout — denying location request",
			zap.String("supi", supi),
		)
		return false
	}
}
