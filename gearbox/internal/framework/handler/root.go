package handler

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
)

// gearPath maps a gear name to its URL path.
func gearPath(name string) string {
	switch name {
	case "haproxy":
		return "/haproxy"
	case "metrics":
		return "/history"
	case "logs":
		return "/logs"
	case "services":
		return "/services"
	case "certificates":
		return "/certificates"
	case "traffic":
		return "/traffic"
	case "alerts":
		return "/alerts"
	case "os_updates":
		return "/os-updates"
	case "containers":
		return "/containers"
	default:
		return ""
	}
}

// RootHandler redirects "/" to the first enabled gear in the nav order.
// Falls back to /settings if no gears are enabled.
func (h *Handler) RootHandler(w http.ResponseWriter, r *http.Request) {
	integrations, ok := auth.GetGearOrderFromContext(r.Context())
	if ok {
		for _, integration := range integrations {
			if integration.Enabled {
				if path := gearPath(integration.Name); path != "" {
					http.Redirect(w, r, path, http.StatusSeeOther)
					return
				}
			}
		}
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
