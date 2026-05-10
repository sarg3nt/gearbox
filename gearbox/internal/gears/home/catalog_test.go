package home

import "testing"

func TestCatalogLoads(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if len(entries) < 20 {
		t.Errorf("expected the catalog to have at least 20 entries, got %d", len(entries))
	}
	// Sanity: every entry should have a slug, name, and icon.
	for _, e := range entries {
		if e.Slug == "" {
			t.Errorf("catalog entry missing slug: %+v", e)
		}
		if e.Name == "" {
			t.Errorf("catalog entry %q missing name", e.Slug)
		}
		if e.IconURL == "" {
			t.Errorf("catalog entry %q missing icon_url", e.Slug)
		}
	}
}

func TestCatalogBySlug(t *testing.T) {
	for _, slug := range []string{"sonarr", "radarr", "plex", "jellyfin"} {
		if _, ok := CatalogBySlug(slug); !ok {
			t.Errorf("expected %q in catalog", slug)
		}
	}
	if _, ok := CatalogBySlug("definitely-not-an-app"); ok {
		t.Errorf("did not expect bogus slug to match")
	}
}

func TestWalk(t *testing.T) {
	doc := map[string]any{
		"appName": "Sonarr",
		"version": "4.0.0",
		"nested":  map[string]any{"deep": "value"},
		"list":    []any{"a", "b", "c"},
	}

	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"appName", "Sonarr", true},
		{"nested.deep", "value", true},
		{"list.0", "a", true},
		{"list.2", "c", true},
		{"list.7", nil, false},
		{"missing", nil, false},
		{"appName.something", nil, false},
	}
	for _, c := range cases {
		got, ok := walk(doc, c.path)
		if ok != c.ok {
			t.Errorf("walk(%q) ok = %v, want %v", c.path, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("walk(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
