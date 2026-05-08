package home

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// HomeIcon returns the SVG icon component for the Home gear sidebar entry.
func HomeIcon() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
			<path stroke-linecap="round" stroke-linejoin="round" d="M3 12l2-2m0 0l7-7 7 7m-9 11V10m-2 11h4a2 2 0 002-2v-7m-8 9H5a2 2 0 01-2-2v-7"></path>
		</svg>`)
		return err
	})
}
