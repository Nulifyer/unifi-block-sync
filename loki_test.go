package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestStaleCollectionPreventsListMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	defer server.Close()
	store := &fakeStore{list: matchingList{Type: "IPV4_ADDRESSES", Items: []matchingItem{{Type: "IP_ADDRESS", Value: "9.9.9.9"}}}}
	worker := syncer{source: lokiClient{baseURL: server.URL, query: "candidates", freshnessQuery: "freshness", maxLogAge: time.Hour, httpClient: server.Client()}, store: store, threshold: 20, maxEntries: 1000}
	if err := worker.run(context.Background()); err == nil {
		t.Fatal("missing collection evidence accepted")
	}
	if store.updates != 0 {
		t.Fatal("stale collection changed firewall list")
	}
}
func TestLokiRejectsNonfiniteCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"client_ip":"8.8.8.8"},"value":[1,"+Inf"]}]}}`)
	}))
	defer server.Close()
	client := lokiClient{baseURL: server.URL, query: "test", httpClient: server.Client()}
	if _, err := client.candidates(context.Background(), 20); err == nil {
		t.Fatal("invalid observation accepted")
	}
}

func TestFreshCollectionAllowsExpiry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "freshness" {
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"%d"]}]}}`, time.Now().Unix())
		} else {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
		}
	}))
	defer server.Close()
	store := &fakeStore{list: matchingList{Type: "IPV4_ADDRESSES", Items: []matchingItem{{Type: "IP_ADDRESS", Value: "9.9.9.9"}}}}
	worker := syncer{source: lokiClient{baseURL: server.URL, query: "candidates", freshnessQuery: "freshness", maxLogAge: time.Hour, httpClient: server.Client()}, store: store, threshold: 20, maxEntries: 1000}
	if err := worker.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.updates != 1 || store.list.Items[0].Value != emptyListSentinel {
		t.Fatal("genuine expiry failed")
	}
}
func TestPartialCandidateWarningDoesNotReplaceList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","warnings":["partial results"],"data":{"resultType":"vector","result":[]}}`)
	}))
	defer server.Close()
	c := lokiClient{baseURL: server.URL, query: "test", httpClient: server.Client()}
	if _, err := c.candidates(context.Background(), 20); err == nil {
		t.Fatal("partial response accepted")
	}
}
