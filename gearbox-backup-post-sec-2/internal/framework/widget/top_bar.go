package widget

import (
	"github.com/a-h/templ"
)

// TopBarControl represents a control that can be displayed in the dashboard's top bar.
type TopBarControl struct {
	// ID is the unique identifier for this control
	ID string

	// Priority determines the order of controls (lower numbers appear first, 0-100)
	// Default priority is 50
	Priority int

	// Component is the templ component that renders the control
	Component templ.Component

	// Group is an optional group name for organizing controls
	// Controls in the same group are rendered together
	Group string
}

// TopBarProvider is an optional interface that widgets can implement
// to register controls in the dashboard's top bar.
type TopBarProvider interface {
	// TopBarControls returns the controls this widget wants to display in the top bar.
	// The context contains information about the dashboard and widget instance.
	TopBarControls(ctx *WidgetRenderContext, config map[string]any) []TopBarControl
}

// TopBarControlRegistry manages top bar controls for a dashboard.
type TopBarControlRegistry struct {
	controls []TopBarControl
}

// NewTopBarControlRegistry creates a new top bar control registry.
func NewTopBarControlRegistry() *TopBarControlRegistry {
	return &TopBarControlRegistry{
		controls: make([]TopBarControl, 0),
	}
}

// Register registers a top bar control.
// If multiple widgets register controls, all controls are shown.
// Controls are sorted by priority (lower numbers first).
func (r *TopBarControlRegistry) Register(control TopBarControl) {
	// Set default priority if not specified
	if control.Priority == 0 {
		control.Priority = 50
	}

	r.controls = append(r.controls, control)
}

// GetControls returns all registered controls sorted by priority.
func (r *TopBarControlRegistry) GetControls() []TopBarControl {
	// Sort controls by priority
	controls := make([]TopBarControl, len(r.controls))
	copy(controls, r.controls)

	// Simple bubble sort by priority
	for i := 0; i < len(controls); i++ {
		for j := i + 1; j < len(controls); j++ {
			if controls[j].Priority < controls[i].Priority {
				controls[i], controls[j] = controls[j], controls[i]
			}
		}
	}

	return controls
}

// GetControlsByGroup returns controls filtered by group.
func (r *TopBarControlRegistry) GetControlsByGroup(group string) []TopBarControl {
	var filtered []TopBarControl
	for _, control := range r.GetControls() {
		if control.Group == group {
			filtered = append(filtered, control)
		}
	}
	return filtered
}

// HasControls returns true if any controls are registered.
func (r *TopBarControlRegistry) HasControls() bool {
	return len(r.controls) > 0
}

// Clear clears all registered controls.
func (r *TopBarControlRegistry) Clear() {
	r.controls = make([]TopBarControl, 0)
}
