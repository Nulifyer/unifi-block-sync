package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUniFiUpdateUsesAPIKeyAndReplacementPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method=%s", r.Method)
		}
		if r.URL.Path != "/proxy/network/integration/v1/sites/site-id/traffic-matching-lists/list-id" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "secret" {
			t.Errorf("X-API-Key=%q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, found := payload["id"]; found {
			t.Error("update payload must not include response-only id")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := unifiClient{baseURL: server.URL, apiKey: "secret", siteID: "site-id", httpClient: server.Client()}
	err := client.updateList(context.Background(), matchingList{
		ID: "list-id", Name: "Auto Blocked IPv4", Type: "IPV4_ADDRESSES",
		Items: []matchingItem{{Type: "IP_ADDRESS", Value: "34.76.109.193"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}
