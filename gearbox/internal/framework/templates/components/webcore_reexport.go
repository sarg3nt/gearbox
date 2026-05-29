package components

import wc "github.com/sarg3nt/webcore/ui/components"

// Type aliases for the moved components' parameter types, so existing
// gearbox call sites using components.SelectOption / TableConfig / TableColumn
// keep compiling against the webcore definitions (identical types).
type (
	SelectOption = wc.SelectOption
	TableConfig  = wc.TableConfig
	TableColumn  = wc.TableColumn
)
