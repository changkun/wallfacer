//go:build ignore

// gen-api-contract generates derived API contract artifacts from the typed
// route definitions in internal/apicontract/routes.go.
//
// Generated files:
//   - docs/internals/api-contract.json — machine-readable route catalogue
//
// The Vue frontend uses literal /api/... paths, so no JS path-builder
// helpers are generated.
//
// Usage (from the repository root):
//
//	go run scripts/gen-api-contract.go
//	# or
//	make api-contract
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"changkun.de/x/wallfacer/internal/apicontract"
)

func main() {
	root := repoRoot()

	// Generate docs/internals/api-contract.json
	contractJSON, err := apicontract.GenerateContractJSON()
	if err != nil {
		log.Fatalf("GenerateContractJSON: %v", err)
	}
	contractPath := filepath.Join(root, "docs", "internals", "api-contract.json")
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", filepath.Dir(contractPath), err)
	}
	if err := os.WriteFile(contractPath, contractJSON, 0o644); err != nil {
		log.Fatalf("write %s: %v", contractPath, err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", contractPath, len(contractJSON))

	fmt.Printf("ok — %d routes\n", len(apicontract.Routes))
}

// repoRoot walks up from this source file to find the repository root
// (the directory that contains go.mod).
func repoRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("runtime.Caller failed")
	}
	// thisFile = .../scripts/gen-api-contract.go → go up one level
	return filepath.Dir(filepath.Dir(thisFile))
}
