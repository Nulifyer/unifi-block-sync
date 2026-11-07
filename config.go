package main

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	LokiURL        string
	LokiQuery      string
	FreshnessQuery string
	MaxLogAge      time.Duration
	LokiUsername   string
	LokiPassword   string
	UniFiURL       string
	UniFiAPIKey    string
	UniFiSiteID    string
	UniFiListID    string
	UniFiListName  string
	UniFiCAFile    string
	Threshold      int
	MaxEntries     int
	PollInterval   time.Duration
	RequestTimeout time.Duration
	DryRun         bool
	InsecureTLS    bool
	AllowCIDRs     []netip.Prefix
}

func loadConfig() (config, error) {
	c := config{
		LokiURL:        strings.TrimRight(os.Getenv("LOKI_URL"), "/"),
		LokiQuery:      envString("LOKI_QUERY", `sum by (client_ip) (count_over_time({job="goipabuse"} | json | __error__="" | event="auth_decision" | decision="blocked" | keep client_ip [30d]))`),
		FreshnessQuery: envString("LOKI_FRESHNESS_QUERY", `max(max_over_time({job="traefik",log_type="access"} | label_format observed="{{ __timestamp__ | unixEpoch }}" | keep observed | unwrap observed [2h]))`),
		MaxLogAge:      envDuration("MAX_LOG_AGE", 2*time.Hour),
		LokiUsername:   os.Getenv("LOKI_USERNAME"),
		LokiPassword:   os.Getenv("LOKI_PASSWORD"),
		UniFiURL:       strings.TrimRight(os.Getenv("UNIFI_URL"), "/"),
		UniFiAPIKey:    os.Getenv("UNIFI_API_KEY"),
		UniFiSiteID:    os.Getenv("UNIFI_SITE_ID"),
		UniFiListID:    os.Getenv("UNIFI_LIST_ID"),
		UniFiListName:  envString("UNIFI_LIST_NAME", "Auto Blocked IPv4"),
		UniFiCAFile:    os.Getenv("UNIFI_CA_FILE"),
		Threshold:      envInt("BLOCK_THRESHOLD", 20),
		MaxEntries:     envInt("MAX_ENTRIES", 1000),
		PollInterval:   envDuration("POLL_INTERVAL", 5*time.Minute),
		RequestTimeout: envDuration("REQUEST_TIMEOUT", 15*time.Second),
		DryRun:         envBool("DRY_RUN", true),
		InsecureTLS:    envBool("UNIFI_INSECURE_TLS", false),
	}

	for name, value := range map[string]string{
		"LOKI_URL": c.LokiURL, "LOKI_QUERY": c.LokiQuery, "UNIFI_URL": c.UniFiURL,
		"UNIFI_API_KEY": c.UniFiAPIKey, "UNIFI_SITE_ID": c.UniFiSiteID,
	} {
		if strings.TrimSpace(value) == "" {
			return config{}, fmt.Errorf("%s is required", name)
		}
	}
	if c.MaxLogAge <= 0 || c.Threshold < 1 || c.MaxEntries < 1 || c.PollInterval <= 0 || c.RequestTimeout <= 0 {
		return config{}, errors.New("thresholds, limits, and durations must be positive")
	}

	for _, raw := range strings.Split(envString("ALLOW_CIDRS", ""), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return config{}, fmt.Errorf("invalid ALLOW_CIDRS entry %q: %w", raw, err)
		}
		c.AllowCIDRs = append(c.AllowCIDRs, prefix.Masked())
	}
	return c, nil
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
