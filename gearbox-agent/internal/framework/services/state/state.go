// Package state manages persistent state between runs.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State holds persistent state for the agent.
type State struct {
	LastCommitSHA string    `json:"last_commit_sha"`
	LastSyncTime  time.Time `json:"last_sync_time"`
	LastError     string    `json:"last_error,omitempty"`
}

// Manager manages state persistence.
type Manager struct {
	mu       sync.RWMutex
	filePath string
	state    *State
}

// NewManager creates a new state manager.
func NewManager(filePath string) (*Manager, error) {
	m := &Manager{
		filePath: filePath,
		state:    &State{},
	}

	// Load existing state if available
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return m, nil
}

// load reads state from file.
func (m *Manager) load() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, m.state)
}

// Save persists state to file. Must be called with lock held.
func (m *Manager) save() error {
	// Ensure directory exists
	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.filePath, data, 0640)
}

// Save persists state to file (thread-safe).
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.save()
}

// GetLastCommitSHA returns the last processed commit SHA.
func (m *Manager) GetLastCommitSHA() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.LastCommitSHA
}

// SetLastCommitSHA sets the last processed commit SHA.
func (m *Manager) SetLastCommitSHA(sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.LastCommitSHA = sha
	m.state.LastSyncTime = time.Now()
	m.state.LastError = ""
	return m.save()
}

// SetError records an error in the state.
func (m *Manager) SetError(err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.state.LastError = err.Error()
	} else {
		m.state.LastError = ""
	}
	return m.save()
}

// GetLastSyncTime returns the last sync time.
func (m *Manager) GetLastSyncTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.LastSyncTime
}

// GetLastError returns the last error message.
func (m *Manager) GetLastError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.LastError
}
