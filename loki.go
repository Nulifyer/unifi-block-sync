package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"
)

type candidate struct {
	Address netip.Addr
	Blocks  int
}

type lokiClient struct {
	baseURL, query, username, password string
	httpClient                         *http.Client
	freshnessQuery                     string
	maxLogAge                          time.Duration
}

func (c lokiClient) candidates(ctx context.Context, threshold int) ([]candidate, error) {
	if c.freshnessQuery != "" {
		timestamp, err := c.latestObservation(ctx)
		if err != nil {
			return nil, err
		}
		if age := time.Since(time.Unix(int64(timestamp), 0)); age > c.maxLogAge || age < -time.Minute {
			return nil, fmt.Errorf("collection observation is stale or in the future")
		}
	}
	endpoint := c.baseURL + "/loki/api/v1/query?" + url.Values{"query": {c.query}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query Loki: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query Loki: status %d", resp.StatusCode)
	}

	var body struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode Loki response: %w", err)
	}
	if body.Status != "success" || body.Data.ResultType != "vector" || len(body.Warnings) > 0 {
		return nil, fmt.Errorf("unexpected Loki response status=%q resultType=%q", body.Status, body.Data.ResultType)
	}

	var out []candidate
	for _, result := range body.Data.Result {
		ip, err := netip.ParseAddr(result.Metric["client_ip"])
		if err != nil || len(result.Value) != 2 {
			return nil, fmt.Errorf("malformed candidate record")
		}
		if !isPublicIPv4(ip) {
			continue
		}
		var rawValue string
		if err := json.Unmarshal(result.Value[1], &rawValue); err != nil {
			return nil, fmt.Errorf("invalid candidate count encoding")
		}
		value, err := strconv.ParseFloat(rawValue, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fmt.Errorf("invalid candidate count")
		}
		if value >= float64(threshold) {
			out = append(out, candidate{Address: ip, Blocks: int(value)})
		}
	}
	return out, nil
}

// A successful empty candidate query may expire the list only while ingestion is fresh.
func (c lokiClient) latestObservation(ctx context.Context) (float64, error) {
	endpoint := c.baseURL + "/loki/api/v1/query?" + url.Values{"query": {c.freshnessQuery}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("check collection freshness: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("collection freshness HTTP %d", resp.StatusCode)
	}
	var body struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if body.Status != "success" || body.Data.ResultType != "vector" || len(body.Warnings) > 0 || len(body.Data.Result) != 1 || len(body.Data.Result[0].Value) != 2 {
		return 0, fmt.Errorf("no reliable recent collection observation")
	}
	var raw string
	if err := json.Unmarshal(body.Data.Result[0].Value[1], &raw); err != nil {
		return 0, err
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0, fmt.Errorf("invalid collection observation")
	}
	return n, nil
}
