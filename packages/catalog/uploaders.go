package catalog

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Uploader is a row of the allowlist: someone who may upload demos through
// the site.
type Uploader struct {
	SteamID string
	Note    string
	AddedAt time.Time
}

// ParseSteamID reads a 64-bit steamid, the 17 digits Steam shows.
func ParseSteamID(s string) (int64, error) {
	if !steamID.MatchString(s) {
		return 0, fmt.Errorf("%q is not a 64-bit steamid (17 digits)", s)
	}
	return strconv.ParseInt(s, 10, 64)
}

// IsUploader reports whether steamid is on the allowlist. Asked on every
// request rather than cached, so adding or removing someone takes effect at
// once on every API instance.
func IsUploader(ctx context.Context, db querier, steamid uint64) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM uploaders WHERE steamid = $1)`, int64(steamid)).Scan(&ok)
	return ok, err
}

// AddUploader puts steamid on the allowlist, or changes its note if it is
// already there. It reports whether the row is new.
func AddUploader(ctx context.Context, db querier, steamid, note string) (bool, error) {
	id, err := ParseSteamID(steamid)
	if err != nil {
		return false, err
	}
	var added bool
	err = db.QueryRow(ctx, `
INSERT INTO uploaders (steamid, note) VALUES ($1, nullif($2, ''))
ON CONFLICT (steamid) DO UPDATE SET note = coalesce(nullif(EXCLUDED.note, ''), uploaders.note)
RETURNING xmax = 0`, id, note).Scan(&added)
	return added, err
}

// RemoveUploader takes steamid off the allowlist. Their demos stay, and an
// upload already queued is still processed. It reports whether they were on
// it.
func RemoveUploader(ctx context.Context, db querier, steamid string) (bool, error) {
	id, err := ParseSteamID(steamid)
	if err != nil {
		return false, err
	}
	tag, err := db.Exec(ctx, `DELETE FROM uploaders WHERE steamid = $1`, id)
	return tag.RowsAffected() > 0, err
}

// Uploaders is the allowlist, oldest first.
func Uploaders(ctx context.Context, db querier) ([]Uploader, error) {
	rows, err := db.Query(ctx, `SELECT steamid, coalesce(note, ''), added_at FROM uploaders ORDER BY added_at, steamid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Uploader
	for rows.Next() {
		var u Uploader
		var id int64
		if err := rows.Scan(&id, &u.Note, &u.AddedAt); err != nil {
			return nil, err
		}
		u.SteamID = strconv.FormatInt(id, 10)
		out = append(out, u)
	}
	return out, rows.Err()
}
