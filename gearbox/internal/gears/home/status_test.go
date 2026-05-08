package home

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		code int
		want TileStatus
	}{
		{200, StatusUp},
		{204, StatusUp},
		{301, StatusUp},
		{401, StatusUp}, // auth-protected endpoints are still "up"
		{403, StatusUp},
		{404, StatusDegraded},
		{418, StatusDegraded},
		{500, StatusDown},
		{502, StatusDown},
		{0, StatusDown},
	}
	for _, c := range cases {
		if got := classify(c.code); got != c.want {
			t.Errorf("classify(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		current int
		want    int
	}{
		{0, 30},
		{30, 60},
		{60, 120},
		{120, 300},
		{300, 300}, // ceiling
		{600, 300}, // anything past ceiling stays at ceiling
	}
	for _, c := range cases {
		if got := nextBackoff(c.current); got != c.want {
			t.Errorf("nextBackoff(%d) = %d, want %d", c.current, got, c.want)
		}
	}
}
