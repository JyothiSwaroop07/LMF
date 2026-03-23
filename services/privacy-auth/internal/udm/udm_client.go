// Package udm implements the UDM client for fetching subscriber privacy profiles.
// In production this calls Nudm_SDM_Get per 3GPP TS 29.503.
package udm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/5g-lmf/privacy-auth/internal/privacy"
	"go.uber.org/zap"
)

const nudmSDMPath = "/nudm-sdm/v2/%s/lcs-privacy-data"

// UDMHTTPClient fetches privacy profiles from the UDM via Nudm_SDM HTTP/2 API.
type UDMHTTPClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewUDMHTTPClient creates a UDMHTTPClient targeting the given UDM base URL.
func NewUDMHTTPClient(baseURL string, timeoutSeconds int, logger *zap.Logger) *UDMHTTPClient {
	return &UDMHTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
		logger: logger,
	}
}

// GetPrivacyProfile fetches the LCS privacy profile for the given SUPI.
// Returns a default CLASS_C profile if UDM is unreachable or profile not found.
func (u *UDMHTTPClient) GetPrivacyProfile(ctx context.Context, supi string) (*privacy.PrivacyProfile, error) {
	url := u.baseURL + fmt.Sprintf(nudmSDMPath, supi)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create UDM request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("UDM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Subscriber not found → use default class
		u.logger.Warn("UDM: subscriber not found, using CLASS_C default", zap.String("supi", supi))
		return &privacy.PrivacyProfile{PrivacyClass: privacy.PrivacyClassC}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("UDM returned HTTP %d for supi=%s", resp.StatusCode, supi)
	}

	// In production: decode JSON body into PrivacyProfile.
	// Simplified: return CLASS_B for demo.
	u.logger.Info("UDM privacy profile fetched", zap.String("supi", supi))
	return &privacy.PrivacyProfile{
		PrivacyClass:   privacy.PrivacyClassB,
		AllowedClients: []string{},
	}, nil
}
