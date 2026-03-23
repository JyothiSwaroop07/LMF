// Package grpc provides gRPC client connections to downstream LMF services.
package grpc

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Clients holds gRPC connections to all downstream positioning and support services.
type Clients struct {
	SessionManager *grpc.ClientConn
	MethodSelector *grpc.ClientConn
	ProtocolHandler *grpc.ClientConn
	GnssEngine      *grpc.ClientConn
	TdoaEngine      *grpc.ClientConn
	EcidEngine      *grpc.ClientConn
	RttEngine       *grpc.ClientConn
	FusionEngine    *grpc.ClientConn
	QosManager      *grpc.ClientConn
	PrivacyAuth     *grpc.ClientConn
}

// Config holds the address of each downstream service.
type Config struct {
	SessionManagerAddr  string
	MethodSelectorAddr  string
	ProtocolHandlerAddr string
	GnssEngineAddr      string
	TdoaEngineAddr      string
	EcidEngineAddr      string
	RttEngineAddr       string
	FusionEngineAddr    string
	QosManagerAddr      string
	PrivacyAuthAddr     string
}

// New dials all downstream services and returns a Clients bundle.
func New(cfg Config) (*Clients, error) {
	dial := func(addr string) (*grpc.ClientConn, error) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, err)
		}
		return conn, nil
	}

	c := &Clients{}
	var err error

	if c.SessionManager, err = dial(cfg.SessionManagerAddr); err != nil {
		return nil, err
	}
	if c.MethodSelector, err = dial(cfg.MethodSelectorAddr); err != nil {
		return nil, err
	}
	if c.ProtocolHandler, err = dial(cfg.ProtocolHandlerAddr); err != nil {
		return nil, err
	}
	if c.GnssEngine, err = dial(cfg.GnssEngineAddr); err != nil {
		return nil, err
	}
	if c.TdoaEngine, err = dial(cfg.TdoaEngineAddr); err != nil {
		return nil, err
	}
	if c.EcidEngine, err = dial(cfg.EcidEngineAddr); err != nil {
		return nil, err
	}
	if c.RttEngine, err = dial(cfg.RttEngineAddr); err != nil {
		return nil, err
	}
	if c.FusionEngine, err = dial(cfg.FusionEngineAddr); err != nil {
		return nil, err
	}
	if c.QosManager, err = dial(cfg.QosManagerAddr); err != nil {
		return nil, err
	}
	if c.PrivacyAuth, err = dial(cfg.PrivacyAuthAddr); err != nil {
		return nil, err
	}

	return c, nil
}

// Close releases all connections.
func (c *Clients) Close() {
	for _, conn := range []*grpc.ClientConn{
		c.SessionManager, c.MethodSelector, c.ProtocolHandler,
		c.GnssEngine, c.TdoaEngine, c.EcidEngine, c.RttEngine,
		c.FusionEngine, c.QosManager, c.PrivacyAuth,
	} {
		if conn != nil {
			conn.Close()
		}
	}
}
