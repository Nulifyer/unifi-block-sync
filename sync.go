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

const emptyListSentinel = "192.0.2.1"

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

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Blocks != candidates[j].Blocks {
			return candidates[i].Blocks > candidates[j].Blocks
		}
		return candidates[i].Address.Compare(candidates[j].Address) < 0
	})
	desired := make([]matchingItem, 0, len(candidates))
	desiredAddresses := make(map[string]struct{}, len(candidates))
	blocksByAddress := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		if candidate.Blocks < s.threshold || !isPublicIPv4(candidate.Address) || allowed(candidate.Address, s.allowCIDRs) {
			continue
		}
		address := candidate.Address.String()
		if _, found := desiredAddresses[address]; found {
			continue
		}
		if len(desired) >= s.maxEntries {
			return fmt.Errorf("MAX_ENTRIES limit %d reached", s.maxEntries)
		}
		desired = append(desired, matchingItem{Type: "IP_ADDRESS", Value: address})
		desiredAddresses[address] = struct{}{}
		blocksByAddress[address] = candidate.Blocks
	}
	if len(desired) == 0 {
		desired = append(desired, matchingItem{Type: "IP_ADDRESS", Value: emptyListSentinel})
		slog.Warn("no candidates qualified; using reserved empty-list sentinel", "client_ip", emptyListSentinel)
	}

	existingCounts := itemCounts(list.Items)
	desiredCounts := itemCounts(desired)
	added, removed, retained := 0, 0, 0
	for key, count := range desiredCounts {
		existingCount := existingCounts[key]
		retained += min(count, existingCount)
		for range count - existingCount {
			added++
			item := itemForKey(desired, key)
			slog.Info("list change planned", "action", "add", "item_type", item.Type, "value", itemDisplay(item), "blocks", blocksByAddress[item.Value], "dry_run", s.dryRun)
		}
	}
	for key, count := range existingCounts {
		for range count - desiredCounts[key] {
			removed++
			item := itemForKey(list.Items, key)
			slog.Info("list change planned", "action", "remove", "item_type", item.Type, "value", itemDisplay(item), "dry_run", s.dryRun)
		}
	}
	if added == 0 && removed == 0 {
		slog.Info("reconciliation complete", "candidates", len(candidates), "added", 0, "removed", 0, "retained", retained, "dry_run", s.dryRun)
		return nil
	}
	if !s.dryRun {
		list.Items = desired
		if err := s.store.updateList(ctx, list); err != nil {
			return err
		}
	}
	slog.Info("reconciliation complete", "candidates", len(candidates), "added", added, "removed", removed, "retained", retained, "dry_run", s.dryRun)
	return nil
}

func itemCounts(items []matchingItem) map[string]int {
	counts := make(map[string]int, len(items))
	for _, item := range items {
		counts[itemKey(item)]++
	}
	return counts
}

func itemKey(item matchingItem) string {
	return item.Type + "\x00" + item.Value + "\x00" + item.Start + "\x00" + item.Stop
}

func itemForKey(items []matchingItem, key string) matchingItem {
	for _, item := range items {
		if itemKey(item) == key {
			return item
		}
	}
	return matchingItem{}
}

func itemDisplay(item matchingItem) string {
	if item.Value != "" {
		return item.Value
	}
	return item.Start + "-" + item.Stop
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
