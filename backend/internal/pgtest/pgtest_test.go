package pgtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigratedGenres(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	var n int
	if err := pool.QueryRow(ctx, `select count(*) from genres`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 9 {
		t.Fatalf("genres %d", n)
	}
}
