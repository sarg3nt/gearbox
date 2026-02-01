// Package services provides adapter interfaces for core framework services.
package services

import (
	"log/slog"

	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// ServerAdapter wraps database access to implement plugin.ServerRegistry.
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
func (a *ServerAdapter) GetEnabledBoxes() []plugin.ServerConfig {
	dbServers, err := a.db.GetEnabledBoxes()
	if err != nil {
		a.logger.Error("failed to get enabled boxes from database", "error", err)
		return a.fallbackServers()
	}

	var servers []plugin.ServerConfig
	for _, dbServer := range dbServers {
		apiKey, _ := a.encryptor.DecryptString(dbServer.APIKeyEncrypted)
		serverConfig := dbServer.ToBoxConfig(apiKey)
		if serverConfig.UsesAgentAPI() {
			servers = append(servers, plugin.ServerConfig{
				ID:       serverConfig.ID,
				Name:     serverConfig.Name,
				AgentURL: serverConfig.AgentURL,
			})
		}
	}
	return servers
}

// GetServer returns a specific server by ID.
func (a *ServerAdapter) GetServer(id string) (*plugin.ServerConfig, bool) {
	servers := a.GetEnabledBoxes()
	for _, srv := range servers {
		if srv.ID == id {
			return &srv, true
		}
	}
	return nil, false
}

// IsPluginEnabled checks if a specific integration/plugin is enabled for a server.
func (a *ServerAdapter) IsPluginEnabled(serverID, integration string) bool {
	enabled, err := a.db.IsPluginEnabled(serverID, integration)
	if err != nil {
		a.logger.Error("failed to check integration status", "server", serverID, "integration", integration, "error", err)
		return true // Default to enabled on error
	}
	return enabled
}

// fallbackServers converts static server configs to plugin.ServerConfig.
func (a *ServerAdapter) fallbackServers() []plugin.ServerConfig {
	var servers []plugin.ServerConfig
	for _, srv := range a.fallback {
		servers = append(servers, plugin.ServerConfig{
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

// GetFullServers implements plugin.FullServerRegistry.
// Returns servers in the plugin-defined FullServerConfig format.
func (a *ServerAdapter) GetFullServers() []plugin.FullServerConfig {
	modelServers := a.GetEnabledServersAsModels()
	result := make([]plugin.FullServerConfig, 0, len(modelServers))
	for _, srv := range modelServers {
		result = append(result, plugin.FullServerConfig{
			ID:       srv.ID,
			Name:     srv.Name,
			AgentURL: srv.AgentURL,
			APIKey:   srv.APIKey,
			Enabled:  true,
		})
	}
	return result
}

// Ensure ServerAdapter implements plugin.ServerRegistry and plugin.FullServerRegistry.
var (
	_ plugin.ServerRegistry     = (*ServerAdapter)(nil)
	_ plugin.FullServerRegistry = (*ServerAdapter)(nil)
)
