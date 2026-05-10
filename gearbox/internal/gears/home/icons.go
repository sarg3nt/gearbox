package home

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// HomeIcon returns the SVG icon component for the Home gear sidebar entry.
//
// Path is the canonical Heroicons v1 "home" outline: a roof with both
// walls reaching it, a small door cut in the bottom-center. The previous
// path was a malformed variant whose right wall stopped halfway up,
// leaving a visual gap (issue: "the right wall is missing"). It also
// drew a stray vertical stroke down the middle.
func HomeIcon() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
			<path stroke-linecap="round" stroke-linejoin="round" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"></path>
		</svg>`)
		return err
	})
}
