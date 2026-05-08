package home

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Handlers serves the Home dashboard pages and APIs.
type Handlers struct {
	deps   gear.Dependencies
	hub    *sseHub
	worker *statusWorker
}

// NewHandlers builds a Handlers using the gear dependencies.
// The SSE hub is created here; the worker is wired up later in Gear.Initialize
// once the worker has been constructed.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps, hub: newSSEHub()}
}

// IndexPage renders a board. The slug URL parameter selects which board;
// when absent (e.g. GET /home/) the default board is used. The default
// board is created lazily on first visit.
func (h *Handlers) IndexPage(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	user := h.getUser(r)
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	slug := chi.URLParam(r, "slug")

	// Resolve the active board.
	var (
		board *database.HomeBoard
		err   error
	)
	if slug == "" {
		// Visiting /home/ — render the user's default-or-first board.
		board, err = h.activeBoardLocked(db)
	} else {
		board, err = db.GetHomeBoardBySlug(slug)
		if err == nil && board == nil {
			http.NotFound(w, r)
			return
		}
	}
	if err != nil {
		h.deps.Logger.Error("resolve home board failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	tiles, err := db.ListHomeTiles(board.ID)
	if err != nil {
		h.deps.Logger.Error("ListHomeTiles failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	boards, err := db.ListHomeBoards()
	if err != nil {
		h.deps.Logger.Error("ListHomeBoards failed", "error", err)
		boards = []database.HomeBoard{*board}
	}

	currentLanding, _ := db.GetUserDefaultLandingPath(user.ID)
	homeIsLanding := currentLanding == "/home" || currentLanding == "/home/" ||
		currentLanding == "/home/b/"+board.Slug

	IndexPage(IndexPageData{
		User:           user,
		Board:          board,
		Tiles:          tiles,
		Boards:         boards,
		HomeIsLanding:  homeIsLanding,
	}).Render(r.Context(), w) //nolint:errcheck
}

// activeBoardLocked returns the default board, creating it on first use.
func (h *Handlers) activeBoardLocked(db *database.DB) (*database.HomeBoard, error) {
	board, err := db.GetHomeBoardBySlug(database.DefaultBoardSlug)
	if err != nil {
		return nil, err
	}
	if board != nil {
		return board, nil
	}
	return db.EnsureDefaultHomeBoard()
}

// IndexPageData is what the templ index page consumes. Defining the shape
// here (rather than spreading parameters) keeps the templ signature stable
// as we add fields like search-engine prefs in later phases.
type IndexPageData struct {
	User          *models.User
	Board         *database.HomeBoard
	Tiles         []database.HomeTile
	Boards        []database.HomeBoard
	HomeIsLanding bool
}

// getUser returns the user from the auth context or a placeholder.
func (h *Handlers) getUser(r *http.Request) *models.User {
	if h.deps.Auth == nil {
		return &models.User{Email: "unknown"}
	}
	u := h.deps.Auth.GetUser(r)
	if u == nil {
		return &models.User{Email: "unknown"}
	}
	return &models.User{ID: u.ID, Email: u.Email}
}
