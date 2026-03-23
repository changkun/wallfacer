// Package apicontract is the single source of truth for all HTTP API route
// definitions in wallfacer.
//
// It defines every API endpoint as a [Route] struct with method, URL pattern, name,
// description, and tags. The canonical [Routes] slice is consumed by the server to
// register handlers, by the code generator to produce frontend route helpers and
// JSDoc types, and by the contract generator to emit machine-readable API documentation.
// Centralizing routes here prevents drift between backend handlers and frontend callers.
//
// # Connected packages
//
// Consumed by [changkun.de/x/wallfacer/internal/cli] (server startup registers
// handlers from Routes) and scripts/gen-api-contract.go (generates frontend JS and
// API contract JSON). When adding or modifying an API route, update Routes here first,
// then re-run make api-contract to regenerate derived artifacts.
//
// # Usage
//
//	for _, r := range apicontract.Routes {
//	    mux.HandleFunc(r.FullPattern(), handlerForRoute(r))
//	}
package apicontract
