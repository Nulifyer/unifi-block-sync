package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
)

var nonPublicIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

type candidateSource interface {
	candidates(context.Context, int) ([]candidate, error)
}

type listStore interface {
	getList(context.Context) (matchingList, error)
	updateList(context.Context, matchingList) error
}

type syncer struct {
	source     candidateSource
	store      listStore
	threshold  int
	maxEntries int
	dryRun     bool
	allowCIDRs []netip.Prefix
}

func (s syncer) run(ctx context.Context) error {
	candidates, err := s.source.candidates(ctx, s.threshold)
	if err != nil {
		return err
	}
	list, err := s.store.getList(ctx)
	if err != nil {
		return err
	}
	if list.Type != "IPV4_ADDRESSES" {
		return fmt.Errorf("list %q has type %q, want IPV4_ADDRESSES", list.Name, list.Type)
	}

	existing := make(map[string]struct{}, len(list.Items))
	for _, item := range list.Items {
		if item.Type == "IP_ADDRESS" {
			existing[item.Value] = struct{}{}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Blocks > candidates[j].Blocks })
	added := 0
	for _, candidate := range candidates {
		if allowed(candidate.Address, s.allowCIDRs) {
			continue
		}
		address := candidate.Address.String()
		if _, found := existing[address]; found {
			continue
		}
		if len(list.Items) >= s.maxEntries {
			return fmt.Errorf("MAX_ENTRIES limit %d reached", s.maxEntries)
		}
		list.Items = append(list.Items, matchingItem{Type: "IP_ADDRESS", Value: address})
		existing[address] = struct{}{}
		added++
		slog.Info("candidate selected", "client_ip", address, "blocks", candidate.Blocks, "dry_run", s.dryRun)
	}
	if added == 0 {
		slog.Info("reconciliation complete", "candidates", len(candidates), "added", 0)
		return nil
	}
	if !s.dryRun {
		if err := s.store.updateList(ctx, list); err != nil {
			return err
		}
	}
	slog.Info("reconciliation complete", "candidates", len(candidates), "added", added, "dry_run", s.dryRun)
	return nil
}

func allowed(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func isPublicIPv4(address netip.Addr) bool {
	if !address.Is4() || !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicIPv4 {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
