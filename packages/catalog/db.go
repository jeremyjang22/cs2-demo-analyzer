package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is what both a pool and a transaction can do.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Enums and dates go through text, so a Row is plain strings and the
// database's enum and date types stay its own business.
const selectRow = `
SELECT name, visibility::text, owner, coalesce(kind::text, ''), coalesce(event, ''),
       coalesce(played_on::text, ''), coalesce(title, ''), sample, coalesce(summary::text, '{}')
FROM demos`

func scanRow(rows interface{ Scan(...any) error }) (Row, error) {
	var r Row
	var owner *int64
	var summary string
	if err := rows.Scan(&r.Name, &r.Visibility, &owner, &r.Kind, &r.Event, &r.Date, &r.Title, &r.Sample, &summary); err != nil {
		return Row{}, err
	}
	if owner != nil {
		r.Owner = strconv.FormatInt(*owner, 10)
	}
	if err := json.Unmarshal([]byte(summary), &r.Summary); err != nil {
		return Row{}, fmt.Errorf("%s: summary: %w", r.Name, err)
	}
	return r, nil
}

// Load is the row published as name, or nil if there is none.
func Load(ctx context.Context, q querier, name string) (*Row, error) {
	r, err := scanRow(q.QueryRow(ctx, selectRow+` WHERE name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Ready is every demo the site can show: processed, with a payload in the
// bucket. Uploads still in flight are rows too, and are left out.
func Ready(ctx context.Context, q querier) ([]Row, error) {
	rows, err := q.Query(ctx, selectRow+` WHERE status = 'ready' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Save writes a published demo - its declaration and what was measured from
// the payload now in the bucket - creating the row or replacing it, and
// marks it ready.
func Save(ctx context.Context, tx pgx.Tx, d Demo, m Measured) error {
	if p := d.Problems(); len(p) > 0 {
		return fmt.Errorf("%s: %s", d.Name, p[0])
	}
	summary, err := json.Marshal(m.Summary)
	if err != nil {
		return err
	}
	owner, err := ownerArg(d.Owner)
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, `
INSERT INTO demos (name, status, visibility, owner, kind, event, played_on, title, sample,
                   summary, map, tick_rate, rounds, parsed_at)
VALUES ($1, 'ready', $2::demo_visibility, $3, $4::demo_kind, $5, $6::date, $7, $8,
        $9::jsonb, $10, $11, $12, now())
ON CONFLICT (name) DO UPDATE SET
    status = 'ready', visibility = excluded.visibility, owner = excluded.owner,
    kind = excluded.kind, event = excluded.event, played_on = excluded.played_on,
    title = excluded.title, sample = excluded.sample, summary = excluded.summary,
    map = excluded.map, tick_rate = excluded.tick_rate, rounds = excluded.rounds,
    parsed_at = now()
RETURNING id::text`,
		d.Name, d.Visibility, owner, null(d.Kind), null(d.Event), null(d.Date), null(d.Title), d.Sample,
		string(summary), m.Map, m.TickRate, m.Rounds).Scan(&id)
	if err != nil {
		return fmt.Errorf("%s: %w", d.Name, err)
	}

	// demo_players makes "every game this steamid is in" one indexed lookup.
	// Rewritten whole: a re-export can change the roster's names.
	if _, err := tx.Exec(ctx, `DELETE FROM demo_players WHERE demo_id = $1`, id); err != nil {
		return err
	}
	for _, p := range m.Roster {
		sid, err := strconv.ParseInt(p.ID, 10, 64)
		if err != nil || sid == 0 {
			continue // a bot, which has no account to find games by
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO demo_players (demo_id, steamid, name, team, kills, deaths, assists)
VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
			id, sid, p.Name, p.Team, p.K, p.D, p.A); err != nil {
			return fmt.Errorf("%s: player %s: %w", d.Name, p.ID, err)
		}
	}
	return nil
}

// Declare replaces a published demo's declaration, leaving what was measured.
func Declare(ctx context.Context, q querier, d Demo) error {
	if p := d.Problems(); len(p) > 0 {
		return fmt.Errorf("%s: %s", d.Name, p[0])
	}
	owner, err := ownerArg(d.Owner)
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
UPDATE demos SET visibility = $2::demo_visibility, owner = $3, kind = $4::demo_kind,
                 event = $5, played_on = $6::date, title = $7, sample = $8
WHERE name = $1`,
		d.Name, d.Visibility, owner, null(d.Kind), null(d.Event), null(d.Date), null(d.Title), d.Sample)
	if err != nil {
		return fmt.Errorf("%s: %w", d.Name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s is not in the catalogue", d.Name)
	}
	return nil
}

// Remove takes a demo out of the catalogue. Its files stay in the bucket,
// unserved: the Worker serves nothing the index does not list.
func Remove(ctx context.Context, q querier, name string) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM demos WHERE name = $1`, name)
	return tag.RowsAffected() > 0, err
}

func null(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ownerArg(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("owner %q: %w", s, err)
	}
	return &n, nil
}
