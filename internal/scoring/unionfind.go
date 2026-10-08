package scoring

// unionFind is a Disjoint Set Union over string keys with path compression
// and union by rank. Accounts that share a device or IP are unioned into one
// set, so a fraud ring collapses to a single connected component.
type unionFind struct {
	parent map[string]string
	rank   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{
		parent: make(map[string]string),
		rank:   make(map[string]int),
	}
}

// find returns the representative of x's set, creating a singleton set the
// first time x is seen. Path compression flattens the tree as it walks.
func (u *unionFind) find(x string) string {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		u.rank[x] = 0
		return x
	}
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x]) // path compression
	}
	return u.parent[x]
}

// union merges the sets containing a and b, attaching the shallower tree
// under the deeper one (union by rank).
func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}

// connected reports whether a and b are in the same set.
func (u *unionFind) connected(a, b string) bool {
	return u.find(a) == u.find(b)
}
