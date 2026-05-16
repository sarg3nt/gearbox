package handler

import (
	"strings"
	"testing"
)

// validateLayoutTiles is a pure function — these tests cover every
// rejection path the PATCH handler relies on (issue #103 review on
// PR #104). Full HTTP-level handler tests would need a wired Handler
// with db + authManager; for the validation surface specifically the
// table tests below match the handler's behaviour 1:1 because the
// handler calls validateLayoutTiles directly and forwards its error
// message verbatim.
func TestValidateLayoutTiles(t *testing.T) {
	good := []layoutTile{
		{ID: "card-cpu", X: 0, Y: 0, W: 6, H: 4},
		{ID: "card-memory", X: 6, Y: 0, W: 6, H: 4},
	}
	if err := validateLayoutTiles(good); err != nil {
		t.Errorf("good tiles rejected: %v", err)
	}

	// GridStack.save() omits w/h when they're at their default of 1,
	// so a 1×N or N×1 tile arrives with W or H decoded as the Go
	// zero value. validateLayoutTiles must normalise those to 1 and
	// accept the tile rather than reject with "w/h must be positive".
	omitted := []layoutTile{
		{ID: "card-cpu", X: 0, Y: 0}, // W and H both omitted (=> 0)
		{ID: "card-mem", X: 1, Y: 0, W: 0, H: 2},
		{ID: "card-net", X: 2, Y: 0, W: 2, H: 0},
	}
	if err := validateLayoutTiles(omitted); err != nil {
		t.Errorf("tiles with omitted w/h rejected: %v", err)
	}
	if omitted[0].W != 1 || omitted[0].H != 1 {
		t.Errorf("expected omitted w/h normalised to 1, got w=%d h=%d", omitted[0].W, omitted[0].H)
	}
	if omitted[1].W != 1 {
		t.Errorf("expected W=0 normalised to 1, got w=%d", omitted[1].W)
	}
	if omitted[2].H != 1 {
		t.Errorf("expected H=0 normalised to 1, got h=%d", omitted[2].H)
	}

	cases := []struct {
		name      string
		tiles     []layoutTile
		wantMatch string
	}{
		{
			name:      "empty array",
			tiles:     nil,
			wantMatch: "at least one tile",
		},
		{
			name:      "empty id",
			tiles:     []layoutTile{{ID: "", X: 0, Y: 0, W: 1, H: 1}},
			wantMatch: "id is empty",
		},
		{
			name: "duplicate id",
			tiles: []layoutTile{
				{ID: "card-cpu", X: 0, Y: 0, W: 6, H: 4},
				{ID: "card-cpu", X: 6, Y: 0, W: 6, H: 4},
			},
			wantMatch: "duplicate id",
		},
		{
			name:      "negative x",
			tiles:     []layoutTile{{ID: "card-cpu", X: -1, Y: 0, W: 1, H: 1}},
			wantMatch: "must be non-negative",
		},
		{
			name:      "negative y",
			tiles:     []layoutTile{{ID: "card-cpu", X: 0, Y: -5, W: 1, H: 1}},
			wantMatch: "must be non-negative",
		},
		{
			name:      "x exceeds bound",
			tiles:     []layoutTile{{ID: "card-cpu", X: maxCoord + 1, Y: 0, W: 1, H: 1}},
			wantMatch: "exceed",
		},
		{
			name:      "negative height",
			tiles:     []layoutTile{{ID: "card-cpu", X: 0, Y: 0, W: 1, H: -1}},
			wantMatch: "w/h must be positive",
		},
		{
			name:      "w exceeds bound",
			tiles:     []layoutTile{{ID: "card-cpu", X: 0, Y: 0, W: maxDim + 1, H: 1}},
			wantMatch: "w/h exceed",
		},
		{
			name:      "x at column boundary",
			tiles:     []layoutTile{{ID: "card-cpu", X: gridColumns, Y: 0, W: 1, H: 1}},
			wantMatch: "x must be <",
		},
		{
			name:      "x+w overflows columns",
			tiles:     []layoutTile{{ID: "card-cpu", X: 8, Y: 0, W: 6, H: 1}},
			wantMatch: "x+w exceeds",
		},
		{
			name:      "too many tiles",
			tiles:     buildOversizedTiles(),
			wantMatch: "maximum",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLayoutTiles(tc.tiles)
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tc.wantMatch)
			}
			if !strings.Contains(err.Error(), tc.wantMatch) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantMatch)
			}
		})
	}
}

// buildOversizedTiles returns one more tile than the cap so the
// "maximum" rejection branch fires.
func buildOversizedTiles() []layoutTile {
	tiles := make([]layoutTile, maxTilesPerLayout+1)
	for i := range tiles {
		tiles[i] = layoutTile{
			ID: pseudoTileID(i),
			X:  0,
			Y:  i * 4,
			W:  1,
			H:  1,
		}
	}
	return tiles
}

// pseudoTileID returns a deterministic unique-ish id for the
// over-cap test. Using a simple letter cycle avoids the duplicate-
// id rejection firing first (which would shadow the cap check).
func pseudoTileID(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	if i < len(alphabet) {
		return "card-" + string(alphabet[i])
	}
	// Two-char IDs for indices past 36; up to 36*36 = 1296 unique
	// values which more than covers maxTilesPerLayout+1.
	a := alphabet[i/len(alphabet)]
	b := alphabet[i%len(alphabet)]
	return "card-" + string([]byte{a, b})
}
