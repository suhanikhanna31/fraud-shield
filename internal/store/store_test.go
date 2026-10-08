package store

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"fraud-shield/internal/models"
)

func alert(id, acct string, at time.Time) models.Alert {
	return models.Alert{ID: id, TransactionID: "tx-" + id, AccountID: acct, Score: 60,
		Reasons: []string{"r1", "r2"}, CreatedAt: at}
}

// storeContract runs the same behavioral checks against any Store, which is
// what makes the PostgreSQL and in-memory implementations interchangeable.
func storeContract(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Millisecond)
	prefix := fmt.Sprintf("acct-%d-", base.UnixNano())
	a1 := prefix + "1"
	a2 := prefix + "2"
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, spec := range []struct{ id, acct string }{{ids[0], a1}, {ids[1], a2}, {ids[2], a1}} {
		if err := s.SaveAlert(ctx, alert(spec.id, spec.acct, base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	got, err := s.ListAlerts(ctx, a1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("filtered list len=%d want 2", len(got))
	}
	if got[0].ID != ids[2] || got[1].ID != ids[0] {
		t.Fatalf("want newest first, got %s then %s", got[0].ID, got[1].ID)
	}
	if !reflect.DeepEqual(got[0].Reasons, []string{"r1", "r2"}) {
		t.Fatalf("reasons round-trip: %v", got[0].Reasons)
	}

	limited, err := s.ListAlerts(ctx, a1, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit not honored: len=%d err=%v", len(limited), err)
	}
}

func TestMemoryStoreContract(t *testing.T) { storeContract(t, NewMemoryStore()) }

func TestMemoryStoreUnfilteredAndDefaultLimit(t *testing.T) {
	m := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		_ = m.SaveAlert(ctx, alert(fmt.Sprint(i), fmt.Sprint("a", i%2), time.Now()))
	}
	all, _ := m.ListAlerts(ctx, "", 0) // 0 => default of 50
	if len(all) != 50 {
		t.Fatalf("default limit: len=%d want 50", len(all))
	}
	if all[0].ID != "59" {
		t.Fatalf("newest first: got %s want 59", all[0].ID)
	}
}

func TestReasonsTextRoundTrip(t *testing.T) {
	cases := [][]string{nil, {"one"}, {"one", "two", "three"}}
	for _, c := range cases {
		got := textToReasons(reasonsToText(c))
		if len(c) == 0 {
			if len(got) != 0 {
				t.Fatalf("empty: got %v", got)
			}
			continue
		}
		if !reflect.DeepEqual(got, c) {
			t.Fatalf("got %v want %v", got, c)
		}
	}
}

// Runs only when DATABASE_URL points at a real Postgres, e.g.
//
//	make db-up && DATABASE_URL=postgres://fraudshield:fraudshield@localhost:5432/fraudshield?sslmode=disable go test ./internal/store
func TestPostgresStoreContract(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres integration test")
	}
	pg, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storeContract(t, pg)
}
