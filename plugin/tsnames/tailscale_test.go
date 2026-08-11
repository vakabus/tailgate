package tailscale

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

type fakeIPNBusWatcher struct {
	notifications []ipn.Notify
	err           error
	closed        bool
}

func (w *fakeIPNBusWatcher) Next() (ipn.Notify, error) {
	if len(w.notifications) == 0 {
		return ipn.Notify{}, w.err
	}
	n := w.notifications[0]
	w.notifications = w.notifications[1:]
	return n, nil
}

func (w *fakeIPNBusWatcher) Close() error {
	w.closed = true
	return nil
}

func TestNextBusBackoff(t *testing.T) {
	// Starting from the minimum, the backoff doubles each step and then
	// saturates at the cap — it must never exceed busBackoffMax, otherwise a
	// reconnect storm would not be throttled.
	cur := busBackoffMin
	want := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		busBackoffMax, // 64s would exceed the 60s cap
		busBackoffMax,
	}
	for i, w := range want {
		cur = nextBusBackoff(cur)
		if cur != w {
			t.Fatalf("step %d: nextBusBackoff = %s, want %s", i, cur, w)
		}
	}
	if got := nextBusBackoff(busBackoffMax); got != busBackoffMax {
		t.Errorf("nextBusBackoff(max) = %s, want %s (must stay capped)", got, busBackoffMax)
	}
}

func TestDirectiveOrder(t *testing.T) {
	positions := make(map[string]int, len(dnsserver.Directives))
	for i, directive := range dnsserver.Directives {
		positions[directive] = i
	}

	for _, downstream := range []string{"cache", "rewrite"} {
		if positions["tsnames"] >= positions[downstream] {
			t.Errorf("tsnames must execute before %s: positions are %d and %d", downstream, positions["tsnames"], positions[downstream])
		}
	}
}

func TestWaitForInitialNetMap(t *testing.T) {
	initial := &netmap.NetworkMap{SelfNode: (&tailcfg.Node{
		ComputedName: "self",
		Addresses:    []netip.Prefix{netip.MustParsePrefix("100.0.0.1/32")},
	}).View()}
	watcher := &fakeIPNBusWatcher{
		// Non-netmap notifications may precede the initial map and must not make
		// startup report ready prematurely.
		notifications: []ipn.Notify{{}, {NetMap: initial}},
		err:           errors.New("unexpected extra read"),
	}
	ts := &Tailscale{zone: "initial.example"}

	if err := ts.waitForInitialNetMap(watcher); err != nil {
		t.Fatalf("waitForInitialNetMap() error = %v", err)
	}
	if _, ok := ts.entries["self"]; !ok {
		t.Fatalf("initial netmap was not installed: entries = %v", ts.entries)
	}
	if len(watcher.notifications) != 0 {
		t.Fatalf("waitForInitialNetMap returned before consuming initial map")
	}
}

func TestWaitForInitialNetMapError(t *testing.T) {
	want := errors.New("bus closed")
	watcher := &fakeIPNBusWatcher{err: want}
	ts := &Tailscale{zone: "initial-error.example"}

	err := ts.waitForInitialNetMap(watcher)
	if !errors.Is(err, want) {
		t.Fatalf("waitForInitialNetMap() error = %v, want wrapping %v", err, want)
	}
	if ts.entries != nil {
		t.Fatalf("entries initialized without a netmap: %v", ts.entries)
	}
}

func TestProcessNetMap(t *testing.T) {
	ts := &Tailscale{zone: "example.com"}

	self := (&tailcfg.Node{
		ComputedName: "self",
		Addresses: []netip.Prefix{
			netip.MustParsePrefix("100.0.0.1/24"),
			netip.MustParsePrefix("fd7a:115c:a1e0::1/128"),
		},
		Tags: []string{"tag:cname-app"},
	}).View()

	nm := &netmap.NetworkMap{
		SelfNode: self,
		Peers: []tailcfg.NodeView{
			(&tailcfg.Node{
				ComputedName: "peer",
				Addresses: []netip.Prefix{
					netip.MustParsePrefix("100.0.0.2/24"),
					netip.MustParsePrefix("fd7a:115c:a1e0::2/128"),
				},
				Tags: []string{"tag:cname-app"},
			}).View(),
			(&tailcfg.Node{
				// shared node should be excluded
				ComputedName: "shared",
				Sharer:       1,
				Addresses: []netip.Prefix{
					netip.MustParsePrefix("100.0.0.3/24"),
					netip.MustParsePrefix("fd7a:115c:a1e0::3/128"),
				},
				Tags: []string{"tag:cname-app"},
			}).View(),
			(&tailcfg.Node{
				// mullvad exit node should be excluded
				ComputedName:    "mullvad",
				IsWireGuardOnly: true,
				Addresses: []netip.Prefix{
					netip.MustParsePrefix("100.0.0.4/24"),
					netip.MustParsePrefix("fd7a:115c:a1e0::4/128"),
				},
				Tags: []string{"tag:cname-app"},
			}).View(),
		},
	}

	want := map[string]map[string][]string{
		"self": {
			"A":    {"100.0.0.1"},
			"AAAA": {"fd7a:115c:a1e0::1"},
		},
		"peer": {
			"A":    {"100.0.0.2"},
			"AAAA": {"fd7a:115c:a1e0::2"},
		},
		"app": {
			"CNAME": {"self.example.com.", "peer.example.com."},
		},
	}

	ts.processNetMap(nm)
	if !cmp.Equal(ts.entries, want) {
		t.Errorf("ts.entries = %v, want %v", ts.entries, want)
	}
	if got := testutil.ToFloat64(entriesGauge.WithLabelValues("example.com")); got != 3 {
		t.Errorf("entries = %v, want 3", got)
	}
	if got := testutil.ToFloat64(netmapUpdatesTotal.WithLabelValues("example.com")); got != 1 {
		t.Errorf("netmapUpdatesTotal = %v, want 1", got)
	}

	// now process another netmap with only self, and make sure peer is removed
	ts.processNetMap(&netmap.NetworkMap{SelfNode: self})
	want = map[string]map[string][]string{
		"self": {
			"A":    {"100.0.0.1"},
			"AAAA": {"fd7a:115c:a1e0::1"},
		},
		"app": {
			"CNAME": {"self.example.com."},
		},
	}
	if !cmp.Equal(ts.entries, want) {
		t.Errorf("ts.entries = %v, want %v", ts.entries, want)
	}
	if got := testutil.ToFloat64(entriesGauge.WithLabelValues("example.com")); got != 2 {
		t.Errorf("entries = %v, want 2", got)
	}
	if got := testutil.ToFloat64(netmapUpdatesTotal.WithLabelValues("example.com")); got != 2 {
		t.Errorf("netmapUpdatesTotal = %v, want 2", got)
	}
}
