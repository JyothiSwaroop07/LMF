package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/sbi-gateway/internal/grpcclient"
	"github.com/5g-lmf/sbi-gateway/internal/handler"
	"github.com/5g-lmf/sbi-gateway/internal/nrf"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading config: %v\n", err)
		os.Exit(1)
	}

	logger, err := middleware.NewLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Start metrics server
	middleware.StartMetricsServer(cfg.Metrics.Port)
	logger.Info("metrics server started", zap.Int("port", cfg.Metrics.Port))

	// Set up gRPC clients
	clients, err := grpcclient.New(cfg, logger) //swaroop added the arguement logger
	if err != nil {
		logger.Fatal("creating grpc clients", zap.Error(err))
	}
	defer clients.Close()

	// Set up Gin router
	if cfg.Log.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	// Log incoming requests at Info level with relevant details (method, path, query, remote addr, etc)
	router.Use(func(c *gin.Context) {
		logger.Info("incoming request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("raw_query", c.Request.URL.RawQuery),
			zap.String("remote_addr", c.Request.RemoteAddr),
			zap.String("host", c.Request.Host),
			zap.String("proto", c.Request.Proto),
			zap.String("content_type", c.Request.Header.Get("Content-Type")),
		)
		c.Next()
		logger.Info("request completed",
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
		)
	})

	// Register handlers
	locationHandler := handler.NewLocationHandler(clients, logger)
	subscriptionHandler := handler.NewSubscriptionHandler(clients, logger)

	// Nllmf API routes (per TS 29.572)
	v1 := router.Group("/nlmf-loc/v1")
	{
		logger.Info("registering API routes", zap.String("prefix", "/nlmf-loc/v1"))
		//swaroop comment

		//changed location-context to determine-location to match the mobileum request
		v1.POST("/determine-location", locationHandler.DetermineLocation)
		v1.DELETE("/determine-location/:lcsSessionRef", locationHandler.CancelLocation)
		v1.POST("/subscriptions", subscriptionHandler.Subscribe)
		v1.DELETE("/subscriptions/:subscriptionId", subscriptionHandler.Unsubscribe)
	}

	// Health endpoints
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})

	// 404 handler for unmatched routes
	router.NoRoute(func(c *gin.Context) {
		logger.Warn("unmatched route - 404",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("host", c.Request.Host),
			zap.String("remote_addr", c.Request.RemoteAddr),
		)
		c.JSON(http.StatusNotFound, gin.H{"error": "route not found", "path": c.Request.URL.Path})
	})

	// // Build HTTP/2 server
	// srv := &http.Server{
	// 	Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
	// 	Handler: router,
	// }

	// // Enable HTTP/2
	// if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
	// 	logger.Fatal("configuring http2", zap.Error(err))
	// }

	h2chandler := h2c.NewHandler(router, &http2.Server{})

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: h2chandler,
	}

	// TLS configuration
	// if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
	// 	tlsCert, err := tls.LoadX509KeyPair(cfg.Server.TLSCert, cfg.Server.TLSKey)
	// 	if err != nil {
	// 		logger.Fatal("loading TLS certificates", zap.Error(err))
	// 	}
	// 	srv.TLSConfig = &tls.Config{
	// 		Certificates: []tls.Certificate{tlsCert},
	// 		MinVersion:   tls.VersionTLS13,
	// 	}
	// }

	// Start server
	// go func() {
	// 	logger.Info("SBI API gateway starting",
	// 		zap.String("addr", srv.Addr),
	// 		zap.Bool("tls", srv.TLSConfig != nil),
	// 	)
	// 	var serveErr error
	// 	if srv.TLSConfig != nil {
	// 		serveErr = srv.ListenAndServeTLS("", "")
	// 	} else {
	// 		serveErr = srv.ListenAndServe()
	// 	}
	// 	if serveErr != nil && serveErr != http.ErrServerClosed {
	// 		logger.Fatal("server failed", zap.Error(serveErr))
	// 	}
	// }()

	// 1. Start http server
	go func() {
		logger.Info("SBI API gateway starting", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// Give the server a moment to bind before registering with NRF
	time.Sleep(200 * time.Millisecond)

	// 2. Register with NRF
	lmfIPv4 := os.Getenv("LMF_SBI_IPV4")
	if lmfIPv4 == "" {
		lmfIPv4 = "127.0.0.1"
	}
	lmfPort := cfg.Server.Port
	if portStr := os.Getenv("LMF_SBI_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			lmfPort = p
		}
	}
	nfInstanceId := os.Getenv("LMF_NF_INSTANCE_ID")
	if nfInstanceId == "" {
		nfInstanceId = "a1b2c3d4-0000-0000-0000-lmf000000001"
	}
	mcc := os.Getenv("LMF_MCC")
	if mcc == "" {
		mcc = "404"
	}
	mnc := os.Getenv("LMF_MNC")
	if mnc == "" {
		mnc = "30"
	}

	nrfURL := os.Getenv("NRF_BASE_URL")
	logger.Info("NRF registration config: %s", zap.String("nrfURL", nrfURL), zap.String("nfInstanceId", nfInstanceId), zap.String("lmfIPv4", lmfIPv4), zap.Int("lmfPort", lmfPort), zap.String("mcc", mcc), zap.String("mnc", mnc))

	registrar := nrf.NewRegistrar(
		nrfURL,
		nfInstanceId,
		lmfIPv4,
		lmfPort,
		mcc, mnc,
		logger,
	)

	heartbeatCtx, heartbeatCancel := context.WithCancel(context.Background())
	defer heartbeatCancel()

	if err := registrar.Register(heartbeatCtx); err != nil {
		// Log as error but don't Fatal
		logger.Error("NRF registration failed, continuing without NRF", zap.Error(err))
	}

	// 3. Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down SBI gateway")

	// 4. Deregister from NRF
	heartbeatCancel() // stop heartbeat goroutine
	registrar.Deregister()

	// 5. Gracefully shutdown HTTP server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
	logger.Info("SBI gateway stopped")
}
