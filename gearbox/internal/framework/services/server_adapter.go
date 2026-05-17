// Package services provides adapter interfaces for core framework services.
package services

import (
	"log/slog"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// ServerAdapter wraps database access to implement gear.ServerRegistry.
type ServerAdapter struct {
	db        *database.DB
	encryptor *crypto.Encryptor
	fallback  []models.BoxConfig
	logger    *slog.Logger

	// capabilities is an optional probe-table cache used by
	// GetEnabledServersWithGearAvailable to filter boxes by which agent
	// gears probed Available. Nil-safe: if unset, capability-aware
	// filtering falls through to the full enabled-server list (fail-open),
	// which preserves current behavior for any caller that hasn't been
	// migrated yet.
	capabilities *agent.CapabilitiesCache
}

// NewServerAdapter creates a new ServerAdapter.
func NewServerAdapter(db *database.DB, encryptor *crypto.Encryptor, fallback []models.BoxConfig, logger *slog.Logger) *ServerAdapter {
	return &ServerAdapter{
		db:        db,
		encryptor: encryptor,
		fallback:  fallback,
		logger:    logger,
	}
}

// GetDB exposes the underlying *database.DB for gears that need to query
// box rows beyond what the gear.ServerRegistry facade exposes (notably the
// Bx gear's status monitor, which needs Location + APIKeyEncrypted to
// probe every agent — fields the trimmed ServerConfig drops).
func (a *ServerAdapter) GetDB() *database.DB {
	return a.db
}

// GetEnabledBoxes returns all boxes that are currently enabled.
func (a *ServerAdapter) GetEnabledBoxes() []gear.ServerConfig {
	dbServers, err := a.db.GetEnabledBoxes()
	if err != nil {
		a.logger.Error("failed to get enabled boxes from database", "error", err)
		return a.fallbackServers()
	}

	var servers []gear.ServerConfig
	for _, dbServer := range dbServers {
		apiKey, _ := a.encryptor.DecryptString(dbServer.APIKeyEncrypted)
		serverConfig := dbServer.ToBoxConfig(apiKey)
		if serverConfig.UsesAgentAPI() {
			servers = append(servers, gear.ServerConfig{
				ID:       serverConfig.ID,
				Name:     serverConfig.Name,
				AgentURL: serverConfig.AgentURL,
			})
		}
	}
	return servers
}

// GetServer returns a specific server by ID.
func (a *ServerAdapter) GetServer(id string) (*gear.ServerConfig, bool) {
	servers := a.GetEnabledBoxes()
	for _, srv := range servers {
		if srv.ID == id {
			return &srv, true
		}
	}
	return nil, false
}

// IsGearEnabled checks if a specific integration/plugin is enabled for a server.
func (a *ServerAdapter) IsGearEnabled(serverID, integration string) bool {
	enabled, err := a.db.IsGearEnabled(serverID, integration)
	if err != nil {
		a.logger.Error("failed to check integration status", "server", serverID, "integration", integration, "error", err)
		return true // Default to enabled on error
	}
	return enabled
}

// fallbackServers converts static server configs to gear.ServerConfig.
func (a *ServerAdapter) fallbackServers() []gear.ServerConfig {
	var servers []gear.ServerConfig
	for _, srv := range a.fallback {
		servers = append(servers, gear.ServerConfig{
			ID:       srv.ID,
			Name:     srv.Name,
			AgentURL: srv.AgentURL,
		})
	}
	return servers
}

// SetCapabilitiesCache wires the dashboard's probe-table cache into the
// adapter so capability-aware methods (GetEnabledServersWithGearAvailable)
// can decide which boxes should appear on a gear-specific page. Optional:
// callers that don't need capability filtering can skip this and the
// adapter degrades to returning the full enabled-server set.
func (a *ServerAdapter) SetCapabilitiesCache(c *agent.CapabilitiesCache) {
	a.capabilities = c
}

// GetEnabledServersWithGearAvailable returns the subset of enabled servers
// whose probe table reports the named **agent** gear as Available. Used by
// the HAProxy / Logs / Services / OS-Updates dashboard pages to skip
// rendering tiles and tabs for boxes the gear physically can't run on
// (e.g. an agent in a distroless container has no haproxy binary, so
// rendering its HAProxy stats tile produces a 503 storm — issue #112).
//
// The gearName argument is the **agent-side** gear name, not the
// dashboard gear name (see dashboardGearToAgentGear in handler/gears.go
// for the mapping). Pass "haproxy", "logs", "services", "metrics",
// "updates", "certificates", or "traffic" as appropriate.
//
// Fail-open contract: a box whose capabilities aren't reachable (agent
// down, no API key, cache miss + fetch failure) is **included** in the
// result. This matches filterGearsByAgentCapabilities so a transient
// agent outage doesn't make gears disappear from the UI. If the
// capability cache hasn't been wired in (SetCapabilitiesCache wasn't
// called), every enabled server is returned — same fail-open posture.
//
// The result is in the same order as GetEnabledServersAsModels.
func (a *ServerAdapter) GetEnabledServersWithGearAvailable(gearName string) []models.BoxConfig {
	all := a.GetEnabledServersAsModels()
	if a.capabilities == nil || gearName == "" {
		return all
	}
	out := make([]models.BoxConfig, 0, len(all))
	for _, srv := range all {
		caps, err := a.capabilities.Get(srv.ID, srv.AgentURL, srv.APIKey)
		if err != nil || caps == nil {
			// Fail-open: include the box if we can't see its probe table.
			out = append(out, srv)
			continue
		}
		entry, present := caps.Entry(gearName)
		if !present {
			// Agent didn't report this gear at all (older agent that
			// pre-dates it). Fail-open — keep the box.
			out = append(out, srv)
			continue
		}
		if entry.IsAvailable() {
			out = append(out, srv)
		}
	}
	return out
}

// GetEnabledServersAsModels returns servers as models.BoxConfig.
// This is a helper for plugins that need to use templates expecting models.BoxConfig.
func (a *ServerAdapter) GetEnabledServersAsModels() []models.BoxConfig {
	dbServers, err := a.db.GetEnabledBoxes()
	if err != nil {
		a.logger.Error("failed to get enabled servers from database", "error", err)
		return a.fallback
	}

	var servers []models.BoxConfig
	for _, dbServer := range dbServers {
		apiKey, _ := a.encryptor.DecryptString(dbServer.APIKeyEncrypted)
		serverConfig := dbServer.ToBoxConfig(apiKey)
		if serverConfig.UsesAgentAPI() {
			servers = append(servers, serverConfig)
		}
	}
	return servers
}

// GetFullServers implements gear.FullServerRegistry.
// Returns servers in the plugin-defined FullServerConfig format.
func (a *ServerAdapter) GetFullServers() []gear.FullServerConfig {
	modelServers := a.GetEnabledServersAsModels()
	result := make([]gear.FullServerConfig, 0, len(modelServers))
	for _, srv := range modelServers {
		result = append(result, gear.FullServerConfig{
			ID:       srv.ID,
			Name:     srv.Name,
			AgentURL: srv.AgentURL,
			APIKey:   srv.APIKey,
			Enabled:  true,
		})
	}
	return result
}

// Ensure ServerAdapter implements gear.ServerRegistry and gear.FullServerRegistry.
var (
	_ gear.ServerRegistry     = (*ServerAdapter)(nil)
	_ gear.FullServerRegistry = (*ServerAdapter)(nil)
)
