package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/5g-lmf/common/config"
	"github.com/5g-lmf/common/middleware"
	"github.com/5g-lmf/sbi-gateway/internal/grpcclient"
	"github.com/5g-lmf/sbi-gateway/internal/handler"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
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

	// Register handlers
	locationHandler := handler.NewLocationHandler(clients, logger)
	subscriptionHandler := handler.NewSubscriptionHandler(clients, logger)

	// Nllmf API routes (per TS 29.572)
	v1 := router.Group("/nlmf-loc/v1")
	{
		logger.Info("registering API routes", zap.String("prefix", "/nlmf-loc/v1"))
		//swaroop comment

		v1.POST("/location-contexts", locationHandler.DetermineLocation)
		v1.DELETE("/location-contexts/:lcsSessionRef", locationHandler.CancelLocation)
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

	// Build HTTP/2 server
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: router,
	}

	// Enable HTTP/2
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		logger.Fatal("configuring http2", zap.Error(err))
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

	go func() {
		logger.Info("SBI API gateway starting", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down SBI gateway")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
	logger.Info("SBI gateway stopped")
}
