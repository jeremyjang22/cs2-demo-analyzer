package catalog

import (
	"context"
	"testing"
)

func TestUploaders(t *testing.T) {
	db, ctx := testDB(t), context.Background()
	if _, err := db.Exec(ctx, `DELETE FROM uploaders`); err != nil {
		t.Fatal(err)
	}
	const me, other = "76561198230376396", "76561198000000001"
	is := func(id uint64) bool {
		t.Helper()
		ok, err := IsUploader(ctx, db, id)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if is(76561198230376396) {
		t.Fatal("an empty list lets someone upload")
	}
	if added, err := AddUploader(ctx, db, me, "owner"); err != nil || !added {
		t.Fatalf("add: %v %v", added, err)
	}
	if !is(76561198230376396) || is(76561198000000001) {
		t.Error("the list does not say who is on it")
	}

	// Adding again keeps the row, and keeps the note unless a new one is given.
	if added, err := AddUploader(ctx, db, me, ""); err != nil || added {
		t.Errorf("re-add: %v %v", added, err)
	}
	if _, err := AddUploader(ctx, db, other, "a friend"); err != nil {
		t.Fatal(err)
	}
	list, err := Uploaders(ctx, db)
	if err != nil || len(list) != 2 || list[0].SteamID != me || list[0].Note != "owner" || list[1].Note != "a friend" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if removed, err := RemoveUploader(ctx, db, other); err != nil || !removed {
		t.Errorf("remove: %v %v", removed, err)
	}
	if removed, _ := RemoveUploader(ctx, db, other); removed {
		t.Error("removed twice")
	}
	if is(76561198000000001) {
		t.Error("still an uploader after removal")
	}

	for _, bad := range []string{"7656119", "7656119823037639x", ""} {
		if _, err := AddUploader(ctx, db, bad, ""); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
