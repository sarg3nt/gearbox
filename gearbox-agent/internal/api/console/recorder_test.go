package console

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorder_WritesAllFrameTypes(t *testing.T) {
	// Smoke test: open a recorder, log every kind of record, close,
	// then parse the file back and confirm each line decodes to the
	// expected envelope. This pins the on-disk format so a future
	// refactor that drops a field is caught here.
	dir := t.TempDir()
	rec, err := OpenRecorder(dir, "test-box", "abcdef12")
	if err != nil {
		t.Fatalf("OpenRecorder: %v", err)
	}
	rec.LogOpen("abcdef12", ModeHostPTY, 0)
	rec.LogIn([]byte("hello\n"))
	rec.LogOut([]byte("world\n"))
	rec.LogResize(132, 50)
	if err := rec.Close("client_close", 0); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Find the file (name includes a timestamp we don't predict).
	entries, _ := os.ReadDir(filepath.Join(dir, "console-sessions"))
	if len(entries) != 1 {
		t.Fatalf("want 1 recording file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "test-box-") {
		t.Errorf("filename = %q, want test-box-... prefix", entries[0].Name())
	}
	if !strings.HasSuffix(entries[0].Name(), "-abcdef12.ndjson") {
		t.Errorf("filename = %q, want session-id suffix", entries[0].Name())
	}

	path := filepath.Join(dir, "console-sessions", entries[0].Name())

	// File mode must be 0600 — recording shells is sensitive.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 0600", st.Mode().Perm())
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	want := []string{"open", "in", "out", "resize", "close"}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var got []string
	var inRec, outRec map[string]any
	for scanner.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("malformed line %q: %v", scanner.Text(), err)
		}
		t, _ := rec["t"].(string)
		got = append(got, t)
		if t == "in" {
			inRec = rec
		}
		if t == "out" {
			outRec = rec
		}
	}
	if scanner.Err() != nil {
		t.Fatalf("scan: %v", scanner.Err())
	}
	if len(got) != len(want) {
		t.Fatalf("frame count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("frame[%d] = %q, want %q", i, got[i], w)
		}
	}

	// Verify base64 round-trip on the in/out payloads.
	inData, _ := inRec["d"].(string)
	dec, err := base64.StdEncoding.DecodeString(inData)
	if err != nil {
		t.Fatalf("in.d not base64: %v", err)
	}
	if string(dec) != "hello\n" {
		t.Errorf("in.d decoded = %q, want %q", dec, "hello\n")
	}
	outData, _ := outRec["d"].(string)
	dec, err = base64.StdEncoding.DecodeString(outData)
	if err != nil {
		t.Fatalf("out.d not base64: %v", err)
	}
	if string(dec) != "world\n" {
		t.Errorf("out.d decoded = %q, want %q", dec, "world\n")
	}
}

func TestRecorder_RefusesEmptyDataDir(t *testing.T) {
	_, err := OpenRecorder("", "box", "sid")
	if err == nil {
		t.Fatal("OpenRecorder with empty dataDir = nil err; want error")
	}
}

func TestRecorder_CloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	rec, err := OpenRecorder(dir, "box", "sid")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Close("a", 0); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := rec.Close("b", 0); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestRecorder_SanitizeForFilename(t *testing.T) {
	// Operators control box names but we still defend against
	// path-traversal characters slipping into the filename.
	cases := []struct {
		in, want string
	}{
		{"", "box"},
		{"prod-01", "prod-01"},
		{"box/with/slashes", "box_with_slashes"},
		{"../escape", "_escape"}, // '/' → '_', then leading dots trimmed
		{".hidden", "hidden"},
		{"weird:chars*", "weird_chars_"},
	}
	for _, c := range cases {
		if got := sanitizeForFilename(c.in); got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
