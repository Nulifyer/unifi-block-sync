package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type matchingItem struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
	Start string `json:"start,omitempty"`
	Stop  string `json:"stop,omitempty"`
}

type matchingList struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Type  string         `json:"type"`
	Items []matchingItem `json:"items"`
}

type unifiClient struct {
	baseURL, apiKey, siteID, listID, listName string
	httpClient                                *http.Client
}

func (c unifiClient) getList(ctx context.Context) (matchingList, error) {
	if c.listID != "" {
		var list matchingList
		err := c.doJSON(ctx, http.MethodGet, c.listPath()+"/"+url.PathEscape(c.listID), nil, &list)
		return list, err
	}

	var response struct {
		Data []matchingList `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.listPath()+"?limit=200", nil, &response); err != nil {
		return matchingList{}, err
	}
	for _, list := range response.Data {
		if list.Name == c.listName {
			return list, nil
		}
	}
	return matchingList{}, fmt.Errorf("UniFi traffic matching list %q not found", c.listName)
}

func (c unifiClient) updateList(ctx context.Context, list matchingList) error {
	payload := struct {
		Name  string         `json:"name"`
		Type  string         `json:"type"`
		Items []matchingItem `json:"items"`
	}{Name: list.Name, Type: list.Type, Items: list.Items}
	return c.doJSON(ctx, http.MethodPut, c.listPath()+"/"+url.PathEscape(list.ID), payload, nil)
}

func (c unifiClient) listPath() string {
	return c.baseURL + "/proxy/network/integration/v1/sites/" + url.PathEscape(c.siteID) + "/traffic-matching-lists"
}

func (c unifiClient) doJSON(ctx context.Context, method, endpoint string, input, output any) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("UniFi request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("UniFi request %s: status %d", method, resp.StatusCode)
	}
	if output != nil {
		if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
			return fmt.Errorf("decode UniFi response: %w", err)
		}
	}
	return nil
}
