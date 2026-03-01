// Package services provides adapter interfaces for core framework services.
package services

import (
	"log/slog"

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
