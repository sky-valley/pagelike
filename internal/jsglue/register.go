package jsglue

import (
	"github.com/sky-valley/pagelike/internal/compose"
	"github.com/sky-valley/pagelike/internal/reactions"
	"github.com/sky-valley/pagelike/internal/schema"
)

func init() { Install() }

// Install connects the shared JavaScript runtime and the schema registry to
// the feature packages' seams (see the package documentation). Linking the
// package (internal/features) installs them.
func Install() {
	schema.SetJSRunner(schemaRunner{})
	compose.SetJSRunner(composeRunner{})
	compose.SetMethodDispatcher(dispatcher{})
	reactions.SetJSRunner(reactionsRunner{})
	reactions.SetKeyIDFunc(schema.SetKeyID)
}

// Uninstall disconnects every seam Install connected: JavaScript slots
// answer as in a build without a runtime (501), composition dispatches
// methods with its own reader of Schema items, and Pagelove.PUT reads keys
// itself. For tests that pin that behaviour; Install restores the wiring.
func Uninstall() {
	schema.SetJSRunner(nil)
	compose.SetJSRunner(nil)
	compose.SetMethodDispatcher(nil)
	reactions.SetJSRunner(nil)
	reactions.SetKeyIDFunc(nil)
}
