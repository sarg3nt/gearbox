package handler

import "net/http"

// NotFoundHandler serves a static, self-contained 404 page for any URL
// that doesn't match a registered route. Wired in main.go via
// chi's r.NotFound().
//
// Deliberately bypasses every dashboard concern beyond the HTTP
// envelope: no templ rendering, no auth middleware, no DB / agent
// lookups, no user-context injection. A page-not-found response for a
// randomly-typed URL must not be a side channel for fingerprinting
// session state or for accidentally exposing data that a logged-out
// caller shouldn't see. The HTML is a const so there's no filesystem
// or template lookup at request time either.
//
// Visually the page matches the dashboard's palette (blue accent,
// system fonts, prefers-color-scheme-aware) and includes a single
// link back to "/". All styles are inline so the response works even
// if the static-asset bundle didn't load.
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 404s shouldn't be cached — the next deploy might add the route.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(notFoundHTML))
}

// notFoundHTML is the entire 404 response body. Kept as a const so the
// handler has no runtime template or filesystem dependency — security
// posture comment on NotFoundHandler explains why this matters.
const notFoundHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>404 — Page not found · Gearbox</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
html, body { height: 100%; margin: 0; }
body {
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  background: #f8fafc;
  color: #0f172a;
  padding: 1.5rem;
}
@media (prefers-color-scheme: dark) {
  body { background: #0f172a; color: #e2e8f0; }
  .footer { color: #64748b; }
}
.card {
  text-align: center;
  max-width: 32rem;
  padding: 2rem;
}
h1 {
  font-size: 5rem;
  font-weight: 700;
  margin: 0 0 0.25rem;
  letter-spacing: -0.025em;
  color: #2563eb;
  line-height: 1;
}
.lead {
  font-size: 1.25rem;
  font-weight: 600;
  margin: 0 0 1rem;
}
p {
  margin: 0.5rem 0;
  line-height: 1.6;
  opacity: 0.85;
}
a.home {
  display: inline-block;
  margin-top: 1.5rem;
  padding: 0.625rem 1.25rem;
  background: #2563eb;
  color: #ffffff;
  text-decoration: none;
  border-radius: 0.5rem;
  font-weight: 500;
  font-size: 0.9375rem;
  transition: background-color 120ms ease;
}
a.home:hover, a.home:focus { background: #1d4ed8; }
.footer {
  margin-top: 2rem;
  font-size: 0.75rem;
  color: #94a3b8;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
</style>
</head>
<body>
  <main class="card">
    <h1>404</h1>
    <p class="lead">Page not found</p>
    <p>The URL you requested isn't registered on this dashboard.</p>
    <a class="home" href="/">Back to dashboard</a>
    <p class="footer">Gearbox</p>
  </main>
</body>
</html>`
