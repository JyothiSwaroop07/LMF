// Package auth validates OAuth 2.0 bearer tokens issued by the NRF per 3GPP TS 29.510.
package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// Claims represents the JWT claims from an NRF-issued access token.
type Claims struct {
	jwt.RegisteredClaims
	Scope       string `json:"scope"`
	NfInstanceID string `json:"nfInstanceId"`
	NfType      string `json:"nfType"`
}

// TokenValidator validates NRF access tokens.
type TokenValidator struct {
	signingKey []byte // HMAC secret or RSA public key material
	logger     *zap.Logger
}

// NewTokenValidator creates a TokenValidator with the given HMAC secret.
func NewTokenValidator(signingKey []byte, logger *zap.Logger) *TokenValidator {
	return &TokenValidator{
		signingKey: signingKey,
		logger:     logger,
	}
}

// Validate parses and validates the bearer token string.
// Returns the extracted claims on success, or an error if invalid/expired.
func (v *TokenValidator) Validate(ctx context.Context, rawToken string) (*Claims, error) {
	rawToken = strings.TrimPrefix(rawToken, "Bearer ")
	rawToken = strings.TrimSpace(rawToken)

	token, err := jwt.ParseWithClaims(rawToken, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.signingKey, nil
	}, jwt.WithExpirationRequired())

	if err != nil {
		return nil, fmt.Errorf("token validation: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Check expiry with a 5-second clock skew tolerance
	if claims.ExpiresAt != nil && time.Until(claims.ExpiresAt.Time) < -5*time.Second {
		return nil, fmt.Errorf("token expired")
	}

	v.logger.Debug("token validated",
		zap.String("nfInstanceId", claims.NfInstanceID),
		zap.String("nfType", claims.NfType),
		zap.String("scope", claims.Scope),
	)

	return claims, nil
}

// HasScope returns true if the claims contain the required scope.
func (v *TokenValidator) HasScope(claims *Claims, requiredScope string) bool {
	scopes := strings.Fields(claims.Scope)
	for _, s := range scopes {
		if s == requiredScope {
			return true
		}
	}
	return false
}
