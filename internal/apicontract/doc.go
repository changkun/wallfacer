// Package apicontract is the single source of truth for all HTTP API route
// definitions in wallfacer.
//
// It defines every API endpoint as a [Route] struct with method, URL pattern, name,
// description, and tags. The canonical [Routes] slice is consumed by the server to
// register handlers and by the contract generator to emit machine-readable API
// documentation. Centralizing routes here prevents drift between backend handlers
// and frontend callers.
//
// # Connected packages
//
// Consumed by [latere.ai/x/wallfacer/internal/cli] (server startup registers
// handlers from Routes) and scripts/gen-api-contract.go (generates
// docs/internals/api-contract.json). When adding or modifying an API route, update
// Routes here first, then re-run make api-contract to regenerate derived artifacts.
// Tests in internal/cli/server_routes_test.go assert that every route in Routes is
// actually registered in the mux, and that the generated contract is not stale.
//
// # Usage
//
//	for _, r := range apicontract.Routes {
//	    mux.HandleFunc(r.FullPattern(), handlerForRoute(r))
//	}
package apicontract
