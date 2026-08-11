package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
)

type candidate struct {
	Address netip.Addr
	Blocks  int
}

type lokiClient struct {
	baseURL, query, username, password string
	httpClient                         *http.Client
}

func (c lokiClient) candidates(ctx context.Context, threshold int) ([]candidate, error) {
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
		Status string `json:"status"`
		Data   struct {
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
	if body.Status != "success" || body.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected Loki response status=%q resultType=%q", body.Status, body.Data.ResultType)
	}

	var out []candidate
	for _, result := range body.Data.Result {
		ip, err := netip.ParseAddr(result.Metric["client_ip"])
		if err != nil || !isPublicIPv4(ip) || len(result.Value) != 2 {
			continue
		}
		var rawValue string
		if err := json.Unmarshal(result.Value[1], &rawValue); err != nil {
			continue
		}
		value, err := strconv.ParseFloat(rawValue, 64)
		if err == nil && int(value) >= threshold {
			out = append(out, candidate{Address: ip, Blocks: int(value)})
		}
	}
	return out, nil
}
