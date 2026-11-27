package catalog

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Against a real, migrated, THROWAWAY database: it deletes every demo.
// TEST_DATABASE_URL=postgres://...; CI points it at the Postgres the
// migrations workflow has just applied every migration to.
func testDB(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated throwaway database")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), `DELETE FROM demos`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestSaveLoadDeclareRemove(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	m, err := Measure(read(t, "testdata/movement.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := Demo{Name: "fixture", Visibility: "private", Owner: "76561198230376396", Kind: "pug", Date: "2026-08-14", Title: "A & B"}

	save := func(d Demo) {
		t.Helper()
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := Save(ctx, tx, d, m); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	save(d)
	save(d) // a re-export replaces, it does not duplicate

	got, err := Load(ctx, db, "fixture")
	if err != nil || got == nil {
		t.Fatalf("Load = %v, %v", got, err)
	}
	if got.Demo != d {
		t.Errorf("declared: got %+v, want %+v", got.Demo, d)
	}
	if got.Summary.Score == nil || *got.Summary.Score != [2]int{1, 3} || got.Summary.Lines["76561198000000001"][4] != 25.3 || len(got.Summary.IDs) != 4 {
		t.Errorf("summary did not survive the database: %+v", got.Summary)
	}
	var players int
	db.QueryRow(ctx, `SELECT count(*) FROM demo_players dp JOIN demos d ON d.id = dp.demo_id WHERE d.name = 'fixture'`).Scan(&players)
	if players != 4 {
		t.Errorf("demo_players: %d rows, want 4", players)
	}

	d.Visibility, d.Sample, d.Kind = "public", true, ""
	if err := Declare(ctx, db, d); err != nil {
		t.Fatal(err)
	}
	rows, err := Ready(ctx, db)
	if err != nil || len(rows) != 1 || rows[0].Demo != d {
		t.Fatalf("Ready = %+v, %v", rows, err)
	}

	// The database refuses a private sample even past Problems().
	if _, err := db.Exec(ctx, `UPDATE demos SET visibility = 'private' WHERE name = 'fixture'`); err == nil ||
		!strings.Contains(err.Error(), "demos_sample_is_public") {
		t.Errorf("private sample: err = %v", err)
	}
	if err := Declare(ctx, db, Demo{Name: "missing", Visibility: "public"}); err == nil {
		t.Error("declaring a demo not in the catalogue should fail")
	}

	if ok, err := Remove(ctx, db, "fixture"); !ok || err != nil {
		t.Errorf("Remove = %v, %v", ok, err)
	}
	if got, _ := Load(ctx, db, "fixture"); got != nil {
		t.Error("still there after Remove")
	}
}
