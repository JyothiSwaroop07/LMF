package nrf

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// NFProfile is the body of the NF profile to be registered with NRF
type NFProfile struct {
	NFInstanceID   string      `json:"nfInstanceId"`
	NFType         string      `json:"nfType"`
	NFStatus       string      `json:"nfStatus"`
	HeartbeatTimer int         `json:"heartbeatTimer"`
	PlmnList       []PlmnId    `json:"plmnList"`
	IPv4Addresses  []string    `json:"ipv4Addresses"`
	AllowedNfTypes []string    `json:"allowedNfTypes"`
	Priority       int         `json:"priority"`
	Capacity       int         `json:"capacity"`
	Load           int         `json:"load"`
	NfServices     []NfService `json:"nfServices"`
}

type PlmnId struct {
	Mcc string `json:"mcc"`
	Mnc string `json:"mnc"`
}

type NfService struct {
	ServiceInstanceID string       `json:"serviceInstanceId"`
	ServiceName       string       `json:"serviceName"`
	Versions          []NFVersion  `json:"versions"`
	Scheme            string       `json:"scheme"`
	NfServiceStatus   string       `json:"nfServiceStatus"`
	IPv4Endpoints     []IpEndpoint `json:"ipEndPoints,omitempty"`
	AllowedNfTypes    []string     `json:"allowedNfTypes"`
	Priority          int          `json:"priority"`
	Capacity          int          `json:"capacity"`
}

type NFVersion struct {
	ApiVersionInURI string `json:"apiVersionInUri"`
	ApiFullVersion  string `json:"apiFullVersion"`
}

// Registrar handles registration of the service with NRF
type Registrar struct {
	nrfBaseURL   string
	nfInstanceId string
	profile      NFProfile
	heartbeatTTL time.Duration
	client       *http.Client
	logger       *zap.Logger

	//stopCh is used to signal the heartbeat goroutine to stop when the service is shutting down
	stopCh chan struct{}
}

type IpEndpoint struct {
	IPv4Address string `json:"ipv4Address"`
	Port        int    `json:"port"`
}

// NewRegistrar creates a new Registrar with the given configuration and logger
//
//	nrfBaseURL    – e.g. "http://192.168.138.23:7777"
//	nfInstanceId  – stable UUID for this LMF instance (store in config or generate once)
//	lmfIPv4       – reachable IP that Open5GS AMF will use to contact this LMF
//	lmfSbiPort    – HTTP port this sbi-gateway listens on (e.g. 8000)
//	mcc, mnc      – must match your Open5GS PLMN (e.g. "001", "01")
func NewRegistrar(nrfBaseURL, nfInstanceId, lmfIPv4 string, lmfSbiPort int, mcc, mnc string, logger *zap.Logger) *Registrar {

	tr := &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
	}

	profile := NFProfile{
		NFInstanceID:   nfInstanceId,
		NFType:         "LMF",
		NFStatus:       "REGISTERED",
		HeartbeatTimer: 10, // seconds
		PlmnList:       []PlmnId{{Mcc: mcc, Mnc: mnc}},
		IPv4Addresses:  []string{lmfIPv4},

		//As of now allowing only AMF to discover LMF, but in future we can allow UDM or other NRF-registered NFs to discover LMF as needed
		AllowedNfTypes: []string{"AMF"},
		Priority:       1,
		Capacity:       100,

		NfServices: []NfService{
			{
				ServiceInstanceID: nfInstanceId + "-nlmf-loc",
				ServiceName:       "nlmf-loc",
				Versions: []NFVersion{
					{ApiVersionInURI: "v1", ApiFullVersion: "1.0.0"},
				},
				Scheme:          "http",
				NfServiceStatus: "REGISTERED",
				IPv4Endpoints: []IpEndpoint{
					{IPv4Address: lmfIPv4, Port: lmfSbiPort},
				},
				AllowedNfTypes: []string{"AMF"},
				Priority:       0,
				Capacity:       100,
			},
		},
	}

	return &Registrar{
		nrfBaseURL:   nrfBaseURL,
		nfInstanceId: nfInstanceId,
		profile:      profile,
		heartbeatTTL: time.Duration(profile.HeartbeatTimer) * time.Second,
		client:       client,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
}

// Registrar sends the initial PUT to NRF to register the NF profile, then starts a goroutine to send periodic heartbeats until stopCh is closed
// need to call this in main.go appropriate place after creating the Registrar instance
// Cancel ctx to stop the heartbeat goroutine when shutting down the service
func (r *Registrar) Register(ctx context.Context) error {
	if err := r.PutProfile(); err != nil {
		return fmt.Errorf("nrf initial registration failed: %w", err)
	}

	r.logger.Info("registered with NRF",
		zap.String("nfInstanceId", r.nfInstanceId),
		zap.String("nrfBaseURL", r.nrfBaseURL),
	)

	// Start heartbeat goroutine
	go r.heartbeatLoop(ctx)
	return nil
}

// Deregister sends DELETE /nnrf-nfm/v1/nf-instances/{id} to NRF.
// Call on graceful shutdown.
func (r *Registrar) Deregister() {
	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", r.nrfBaseURL, r.nfInstanceId)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		r.logger.Error("building nrf deregister request", zap.Error(err))
		return
	}
	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Error("nrf deregister request failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	r.logger.Info("deregistered from NRF", zap.Int("status", resp.StatusCode))
}

// PutProfile sends PUT /nnrf-nfm/v1/nf-instances/{id} with the full NFProfile.
// Used for both initial registration and heartbeat (TS 29.510 §5.2.2.2).
func (r *Registrar) PutProfile() error {
	body, err := json.Marshal(r.profile)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", r.nrfBaseURL, r.nfInstanceId)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain

	// 200 = updated, 201 = created — both are success per TS 29.510
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("NRF returned %d", resp.StatusCode)
	}
	return nil
}

// heartbeatLoop re-PUTs the profile every HeartBeatTimer seconds.
// Open5GS NRF uses the heartbeat timer from the profile; a missed PUT
// causes it to mark the NF as SUSPENDED after ~3× the timer.
func (r *Registrar) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(r.heartbeatTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.PutProfile(); err != nil {
				r.logger.Warn("nrf heartbeat failed", zap.Error(err))
				// keep trying — transient NRF restarts are common in lab setups
			} else {
				r.logger.Debug("nrf heartbeat ok")
			}
		}
	}
}
