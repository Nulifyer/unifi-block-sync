package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig, err := makeTLSConfig(cfg)
	if err != nil {
		slog.Error("configure TLS", "error", err)
		os.Exit(1)
	}
	transport.TLSClientConfig = tlsConfig
	client := &http.Client{Transport: transport, Timeout: cfg.RequestTimeout}
	worker := syncer{
		source:    lokiClient{baseURL: cfg.LokiURL, query: cfg.LokiQuery, freshnessQuery: cfg.FreshnessQuery, maxLogAge: cfg.MaxLogAge, username: cfg.LokiUsername, password: cfg.LokiPassword, httpClient: client},
		store:     unifiClient{baseURL: cfg.UniFiURL, apiKey: cfg.UniFiAPIKey, siteID: cfg.UniFiSiteID, listID: cfg.UniFiListID, listName: cfg.UniFiListName, httpClient: client},
		threshold: cfg.Threshold, maxEntries: cfg.MaxEntries, dryRun: cfg.DryRun, allowCIDRs: cfg.AllowCIDRs,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	run := func() {
		runCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout*2)
		defer cancel()
		if err := worker.run(runCtx); err != nil {
			slog.Error("reconciliation failed", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutdown complete")
			return
		case <-ticker.C:
			run()
		}
	}
}

func makeTLSConfig(cfg config) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureTLS} //nolint:gosec -- explicit opt-in for local diagnosis
	if cfg.UniFiCAFile == "" {
		return tlsConfig, nil
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate pool: %w", err)
	}
	pem, err := os.ReadFile(cfg.UniFiCAFile)
	if err != nil {
		return nil, fmt.Errorf("read UNIFI_CA_FILE: %w", err)
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("UNIFI_CA_FILE contains no certificates")
	}
	tlsConfig.RootCAs = roots
	return tlsConfig, nil
}
