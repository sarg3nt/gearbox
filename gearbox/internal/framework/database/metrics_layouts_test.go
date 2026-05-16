package database

import (
	"bytes"
	"errors"
	"testing"
)

func TestGetMetricsLayoutMissingReturnsSentinel(t *testing.T) {
	// First read on an empty table must return ErrNoMetricsLayout so
	// the handler can translate to 204 No Content — anything else
	// would mask the "no saved layout, use template defaults" path.
	db := setupTestDB(t)

	_, err := db.GetMetricsLayout("user-1", "box-1")
	if !errors.Is(err, ErrNoMetricsLayout) {
		t.Errorf("expected ErrNoMetricsLayout, got %v", err)
	}
}

func TestSaveAndGetMetricsLayout(t *testing.T) {
	// Round-trip the JSON blob byte-for-byte — the storage layer is
	// agnostic to GridStack's payload shape, so the bytes coming back
	// out must match the bytes going in.
	db := setupTestDB(t)

	payload := []byte(`[{"id":"card-cpu","x":0,"y":0,"w":6,"h":4},{"id":"card-memory","x":6,"y":0,"w":6,"h":4}]`)
	if err := db.SaveMetricsLayout("user-1", "box-1", payload); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := db.GetMetricsLayout("user-1", "box-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got.Layout, payload) {
		t.Errorf("layout round-trip = %s, want %s", got.Layout, payload)
	}
	if got.UserID != "user-1" || got.ServerID != "box-1" {
		t.Errorf("metadata wrong: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set by SaveMetricsLayout")
	}
}

func TestSaveMetricsLayoutUpserts(t *testing.T) {
	// PK is (user_id, server_id); a second save for the same pair
	// must replace the layout, not error or create a second row.
	db := setupTestDB(t)

	first := []byte(`[{"id":"card-cpu","x":0,"y":0,"w":6,"h":4}]`)
	second := []byte(`[{"id":"card-cpu","x":6,"y":0,"w":6,"h":4}]`)

	if err := db.SaveMetricsLayout("u", "b", first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := db.SaveMetricsLayout("u", "b", second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, err := db.GetMetricsLayout("u", "b")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got.Layout, second) {
		t.Errorf("expected second save to win, got %s", got.Layout)
	}
}

func TestMetricsLayoutIsolatedPerUserAndBox(t *testing.T) {
	// (user_id, server_id) is the PK — different users on the same
	// box, or the same user on different boxes, must keep separate
	// layouts.
	db := setupTestDB(t)

	aliceBox1 := []byte(`[{"id":"card-cpu","x":1,"y":0,"w":6,"h":4}]`)
	bobBox1 := []byte(`[{"id":"card-cpu","x":2,"y":0,"w":6,"h":4}]`)
	aliceBox2 := []byte(`[{"id":"card-cpu","x":3,"y":0,"w":6,"h":4}]`)

	if err := db.SaveMetricsLayout("alice", "box-1", aliceBox1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMetricsLayout("bob", "box-1", bobBox1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveMetricsLayout("alice", "box-2", aliceBox2); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		user, box string
		want      []byte
	}{
		{"alice", "box-1", aliceBox1},
		{"bob", "box-1", bobBox1},
		{"alice", "box-2", aliceBox2},
	}
	for _, tc := range cases {
		got, err := db.GetMetricsLayout(tc.user, tc.box)
		if err != nil {
			t.Errorf("(%s,%s) get: %v", tc.user, tc.box, err)
			continue
		}
		if !bytes.Equal(got.Layout, tc.want) {
			t.Errorf("(%s,%s) layout = %s, want %s", tc.user, tc.box, got.Layout, tc.want)
		}
	}
}

func TestDeleteMetricsLayoutResetsToDefault(t *testing.T) {
	// Delete must drop the row so the subsequent Get returns
	// ErrNoMetricsLayout — that's what drives the "fall back to
	// template defaults" path after the operator hits Reset.
	db := setupTestDB(t)

	payload := []byte(`[{"id":"card-cpu","x":0,"y":0,"w":6,"h":4}]`)
	if err := db.SaveMetricsLayout("u", "b", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetMetricsLayout("u", "b"); err != nil {
		t.Fatalf("pre-delete get: %v", err)
	}
	if err := db.DeleteMetricsLayout("u", "b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := db.GetMetricsLayout("u", "b")
	if !errors.Is(err, ErrNoMetricsLayout) {
		t.Errorf("expected ErrNoMetricsLayout after delete, got %v", err)
	}
}

func TestDeleteMetricsLayoutIsNoOpWhenAbsent(t *testing.T) {
	// Reset on a box with no saved layout shouldn't surface as an
	// error — the user clicked the button; the desired state is
	// "no row", which is already true.
	db := setupTestDB(t)

	if err := db.DeleteMetricsLayout("u", "never-saved"); err != nil {
		t.Errorf("delete of absent row should be no-op, got %v", err)
	}
}
