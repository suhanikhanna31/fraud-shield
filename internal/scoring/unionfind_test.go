package scoring

import (
	"fmt"
	"testing"
)

func TestUnionFindSingletons(t *testing.T) {
	u := newUnionFind()
	if u.connected("a", "b") {
		t.Fatal("unrelated keys must not be connected")
	}
	if u.find("a") != "a" {
		t.Fatal("a new key should be its own representative")
	}
}

func TestUnionFindTransitiveConnectivity(t *testing.T) {
	u := newUnionFind()
	u.union("a", "b")
	u.union("c", "d")
	if u.connected("a", "c") {
		t.Fatal("a and c should be separate before bridging")
	}
	u.union("b", "c")
	for _, k := range []string{"b", "c", "d"} {
		if !u.connected("a", k) {
			t.Fatalf("a should be connected to %s", k)
		}
	}
}

func TestUnionFindIdempotent(t *testing.T) {
	u := newUnionFind()
	u.union("a", "b")
	root, rank := u.find("a"), u.rank[u.find("a")]
	u.union("a", "b")
	u.union("b", "a")
	if u.find("a") != root || u.rank[root] != rank {
		t.Fatal("re-unioning the same pair must not change structure")
	}
}

func TestUnionFindPathCompression(t *testing.T) {
	u := newUnionFind()
	// Hand-build a deep chain n0 <- n1 <- n2 <- ... to observe compression.
	const n = 50
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("n%d", i)
		u.parent[k], u.rank[k] = k, 0
	}
	for i := 1; i < n; i++ {
		u.parent[fmt.Sprintf("n%d", i)] = fmt.Sprintf("n%d", i-1)
	}
	deepest := fmt.Sprintf("n%d", n-1)
	if got := u.find(deepest); got != "n0" {
		t.Fatalf("root=%s want n0", got)
	}
	// After one find, every node on the path points straight at the root.
	for i := 1; i < n; i++ {
		k := fmt.Sprintf("n%d", i)
		if u.parent[k] != "n0" {
			t.Fatalf("%s parent=%s, want n0 after path compression", k, u.parent[k])
		}
	}
}

func TestUnionFindUnionByRank(t *testing.T) {
	u := newUnionFind()
	// Build a rank-1 tree {a,b} and a singleton c.
	u.union("a", "b")
	big := u.find("a")
	if u.rank[big] != 1 {
		t.Fatalf("rank=%d want 1", u.rank[big])
	}
	// The shallower tree must hang under the deeper one regardless of arg order.
	u.union("c", "a")
	if u.find("c") != big {
		t.Fatal("singleton should attach under the higher-rank root")
	}
	if u.rank[big] != 1 {
		t.Fatalf("rank should not grow when attaching a shallower tree, got %d", u.rank[big])
	}
	// Equal ranks: rank of the new root increases by one.
	u2 := newUnionFind()
	u2.union("a", "b")
	u2.union("c", "d")
	u2.union("a", "c")
	if r := u2.rank[u2.find("a")]; r != 2 {
		t.Fatalf("rank=%d want 2 after merging two rank-1 trees", r)
	}
}

func TestUnionFindClustersByDeviceAndIP(t *testing.T) {
	u := newUnionFind()
	// acc1 & acc2 share a device; acc2 & acc3 share an IP => one ring of 3.
	u.union("acc1", "device:d1")
	u.union("acc2", "device:d1")
	u.union("acc2", "ip:1.2.3.4")
	u.union("acc3", "ip:1.2.3.4")
	u.union("acc9", "device:other")
	if !u.connected("acc1", "acc3") {
		t.Fatal("acc1 and acc3 should be clustered through acc2")
	}
	if u.connected("acc1", "acc9") {
		t.Fatal("acc9 shares nothing and must stay separate")
	}
}
