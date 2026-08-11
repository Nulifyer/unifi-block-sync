package main

import (
	"context"
	"net/netip"
	"testing"
)

type fakeSource []candidate

func (f fakeSource) candidates(context.Context, int) ([]candidate, error) { return f, nil }

type fakeStore struct {
	list    matchingList
	updates int
}

func (f *fakeStore) getList(context.Context) (matchingList, error) { return f.list, nil }
func (f *fakeStore) updateList(_ context.Context, list matchingList) error {
	f.list, f.updates = list, f.updates+1
	return nil
}

func TestSyncAddsOnlyNewAllowedCandidates(t *testing.T) {
	store := &fakeStore{list: matchingList{ID: "list", Name: "Auto Blocked IPv4", Type: "IPV4_ADDRESSES", Items: []matchingItem{{Type: "IP_ADDRESS", Value: "1.1.1.1"}}}}
	worker := syncer{
		source: fakeSource{
			{Address: netip.MustParseAddr("1.1.1.1"), Blocks: 100},
			{Address: netip.MustParseAddr("8.8.8.8"), Blocks: 80},
			{Address: netip.MustParseAddr("9.9.9.9"), Blocks: 70},
		},
		store: store, threshold: 20, maxEntries: 10,
		allowCIDRs: []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24")},
	}
	if err := worker.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.updates != 1 {
		t.Fatalf("updates=%d, want 1", store.updates)
	}
	if got := store.list.Items[len(store.list.Items)-1].Value; got != "9.9.9.9" {
		t.Fatalf("added %q", got)
	}
}

func TestDryRunDoesNotUpdate(t *testing.T) {
	store := &fakeStore{list: matchingList{ID: "list", Name: "Auto Blocked IPv4", Type: "IPV4_ADDRESSES"}}
	worker := syncer{source: fakeSource{{Address: netip.MustParseAddr("9.9.9.9"), Blocks: 30}}, store: store, threshold: 20, maxEntries: 10, dryRun: true}
	if err := worker.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.updates != 0 {
		t.Fatalf("updates=%d, want 0", store.updates)
	}
}

func TestIsPublicIPv4(t *testing.T) {
	for _, test := range []struct {
		address string
		want    bool
	}{
		{"34.76.109.193", true},
		{"10.0.0.1", false},
		{"100.64.0.1", false},
		{"192.0.2.1", false},
		{"::1", false},
	} {
		if got := isPublicIPv4(netip.MustParseAddr(test.address)); got != test.want {
			t.Errorf("isPublicIPv4(%s)=%v, want %v", test.address, got, test.want)
		}
	}
}
