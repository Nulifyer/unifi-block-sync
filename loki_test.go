package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLokiCandidatesParsesVectorAndRejectsNonPublicAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "test query" {
			t.Errorf("query=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"client_ip":"34.76.109.193"},"value":[1,"25"]},{"metric":{"client_ip":"10.0.0.1"},"value":[1,"100"]},{"metric":{"client_ip":"8.8.8.8"},"value":[1,"5"]}]}}`)
	}))
	defer server.Close()

	client := lokiClient{baseURL: server.URL, query: "test query", httpClient: server.Client()}
	got, err := client.candidates(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Address.String() != "34.76.109.193" || got[0].Blocks != 25 {
		t.Fatalf("candidates=%v", got)
	}
}
