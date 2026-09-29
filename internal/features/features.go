// Package features links every optional feature package into the binary.
// Each feature package registers itself with server.Extend from an init
// function; importing this package (from cmd/pagelike and the harness) is
// what enables them. Keep one blank import per line, sorted.
package features

import (
	_ "github.com/sky-valley/pagelike/internal/compose"
	_ "github.com/sky-valley/pagelike/internal/participation"
	// jsglue connects the server JavaScript runtime to schemas, composition
	// and reactions, and composition's method elements to the schema
	// registry.
	_ "github.com/sky-valley/pagelike/internal/jsglue"
	_ "github.com/sky-valley/pagelike/internal/query"
	// jsrt links the server JavaScript worker into every binary that links
	// the features (the pagelike binary, the harness, test binaries), so a
	// Runtime's pool can start os.Executable() as a worker.
	_ "github.com/sky-valley/pagelike/internal/jsrt"
	_ "github.com/sky-valley/pagelike/internal/reactions"
	_ "github.com/sky-valley/pagelike/internal/schema"
	_ "github.com/sky-valley/pagelike/internal/server"
)
