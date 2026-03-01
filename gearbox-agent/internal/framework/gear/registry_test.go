package gear

import (
	"testing"
)

// TestPluginRegistration verifies that plugins can be registered.
func TestPluginRegistration(t *testing.T) {
	// Create a test registry
	testReg := &Registry{
		plugins: make(map[string]Gear),
	}

	// Create a mock plugin
	mockPlugin := &mockPlugin{
		info: Info{
			Name:        "test-plugin",
			DisplayName: "Test Plugin",
			Description: "A test plugin",
			Version:     "1.0.0",
			Category:    "test",
			Core:        false,
		},
	}

	// Register the plugin
	testReg.Register(mockPlugin)

	// Verify registration
	if testReg.Count() != 1 {
		t.Errorf("Expected 1 plugin, got %d", testReg.Count())
	}

	// Verify we can retrieve it
	p, ok := testReg.Get("test-plugin")
	if !ok {
		t.Error("Expected to find test-plugin")
	}

	if p.Info().Name != "test-plugin" {
		t.Errorf("Expected plugin name test-plugin, got %s", p.Info().Name)
	}
}

// mockPlugin implements Plugin interface for testing.
type mockPlugin struct {
	BaseGear
	info Info
}

func (m *mockPlugin) Info() Info {
	return m.info
}
