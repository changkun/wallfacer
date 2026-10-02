package testenv

import (
	"os"
	"testing"
)

// TestMain runs this package's tests under RunIsolated itself, so the guard
// tests here check the environment the other packages' tests get.
func TestMain(m *testing.M) { os.Exit(RunIsolated(m)) }
