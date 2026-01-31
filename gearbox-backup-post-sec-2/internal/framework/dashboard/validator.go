package dashboard

import "fmt"

// Validator validates dashboard configurations.
type Validator struct{}

// NewValidator creates a new dashboard validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate validates a dashboard configuration.
func (v *Validator) Validate(d *Dashboard) ValidationErrors {
	var errors ValidationErrors

	// Validate version
	if d.Version == "" {
		errors = append(errors, ValidationError{
			Field:   "version",
			Message: "version is required",
		})
	}

	// Validate name
	if d.Name == "" {
		errors = append(errors, ValidationError{
			Field:   "name",
			Message: "name is required",
		})
	}

	// Validate created_by
	if d.CreatedBy == "" {
		errors = append(errors, ValidationError{
			Field:   "created_by",
			Message: "created_by is required",
		})
	}

	// Validate layout
	if err := v.validateLayout(&d.Layout); err != nil {
		errors = append(errors, err...)
	}

	// Validate widgets (allow empty dashboards)
	for i, widget := range d.Widgets {
		if err := v.validateWidget(&widget, i); err != nil {
			errors = append(errors, err...)
		}
	}

	return errors
}

// validateLayout validates the dashboard layout configuration.
func (v *Validator) validateLayout(l *Layout) ValidationErrors {
	var errors ValidationErrors

	// Validate columns (1-12)
	if l.Columns < 1 || l.Columns > 12 {
		errors = append(errors, ValidationError{
			Field:   "layout.columns",
			Message: "columns must be between 1 and 12",
		})
	}

	// Validate gap (0-16 for Tailwind spacing)
	if l.Gap < 0 || l.Gap > 16 {
		errors = append(errors, ValidationError{
			Field:   "layout.gap",
			Message: "gap must be between 0 and 16",
		})
	}

	return errors
}

// validateWidget validates a widget configuration.
func (v *Validator) validateWidget(w *Widget, index int) ValidationErrors {
	var errors ValidationErrors
	prefix := fmt.Sprintf("widgets[%d]", index)

	// Validate ID
	if w.ID == "" {
		errors = append(errors, ValidationError{
			Field:   prefix + ".id",
			Message: "widget ID is required",
		})
	}

	// Validate type
	if w.Type == "" {
		errors = append(errors, ValidationError{
			Field:   prefix + ".type",
			Message: "widget type is required",
		})
	}

	// Validate position
	if err := v.validatePosition(&w.Position, prefix); err != nil {
		errors = append(errors, err...)
	}

	return errors
}

// validatePosition validates a widget position.
func (v *Validator) validatePosition(p *WidgetPosition, prefix string) ValidationErrors {
	var errors ValidationErrors

	// Validate row (must be >= 1)
	if p.Row < 1 {
		errors = append(errors, ValidationError{
			Field:   prefix + ".position.row",
			Message: "row must be >= 1",
		})
	}

	// Validate column (must be >= 1)
	if p.Column < 1 {
		errors = append(errors, ValidationError{
			Field:   prefix + ".position.column",
			Message: "column must be >= 1",
		})
	}

	// Validate width (must be >= 1)
	if p.Width < 1 {
		errors = append(errors, ValidationError{
			Field:   prefix + ".position.width",
			Message: "width must be >= 1",
		})
	}

	// Validate height (must be positive int or "auto", or nil for default)
	if p.Height != nil {
		switch h := p.Height.(type) {
		case string:
			if h != "auto" && h != "" {
				errors = append(errors, ValidationError{
					Field:   prefix + ".position.height",
					Message: "height must be a positive number or 'auto'",
				})
			}
		case int:
			if h <= 0 {
				errors = append(errors, ValidationError{
					Field:   prefix + ".position.height",
					Message: "height must be a positive number or 'auto'",
				})
			}
		case float64:
			if h <= 0 {
				errors = append(errors, ValidationError{
					Field:   prefix + ".position.height",
					Message: "height must be a positive number or 'auto'",
				})
			}
		default:
			errors = append(errors, ValidationError{
				Field:   prefix + ".position.height",
				Message: "height must be a positive number or 'auto'",
			})
		}
	}

	return errors
}

// ValidateWidgetIDs checks for duplicate widget IDs.
func (v *Validator) ValidateWidgetIDs(widgets []Widget) ValidationErrors {
	var errors ValidationErrors
	seen := make(map[string]bool)

	for i, widget := range widgets {
		if seen[widget.ID] {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("widgets[%d].id", i),
				Message: fmt.Sprintf("duplicate widget ID: %s", widget.ID),
			})
		}
		seen[widget.ID] = true
	}

	return errors
}
