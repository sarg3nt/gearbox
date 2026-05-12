package bx

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"
)

// bxIconSVG is the sidebar icon for the Bx gear: a 2×2 grid of rounded
// rectangles suggesting "many boxes," echoing the boxes-as-cards layout
// of the Bx fleet page. Built to render at 24×24 to match the other
// sidebar icons (Heroicons-style, stroke-width 1.6).
const bxIconSVG = `<rect x="3" y="3" width="7.5" height="7.5" rx="1.2"/>` +
	`<rect x="13.5" y="3" width="7.5" height="7.5" rx="1.2"/>` +
	`<rect x="3" y="13.5" width="7.5" height="7.5" rx="1.2"/>` +
	`<rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.2"/>`

// BxIcon returns the Bx SVG sized to match the sidebar (w-6 h-6).
func BxIcon() templ.Component {
	return BxIconClass("w-6 h-6 flex-shrink-0")
}

// BxIconClass returns the Bx SVG with caller-supplied Tailwind classes so
// the chip in the header (smaller) and any future hero render (larger) can
// reuse the same artwork.
func BxIconClass(class string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, `<svg class=%q fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6">%s</svg>`, class, bxIconSVG)
		return err
	})
}
