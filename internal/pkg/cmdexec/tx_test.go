package cmdexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTx_AllSucceed(t *testing.T) {
	tx := NewTx()
	tx.Add(New("true"))
	tx.Add(New("true"))
	if err := tx.Run(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestTx_StepFailsRunsRollback(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "rollback-ran")

	tx := NewTx()
	tx.AddWithRollback(
		New("false"),         // fails immediately
		New("touch", marker), // rollback: create marker file
	)
	err := tx.Run()
	if err == nil {
		t.Fatal("expected error")
	}

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatal("rollback marker file should exist")
	}

	var txErr *TxError
	if !errors.As(err, &txErr) {
		t.Fatalf("expected *TxError, got %T", err)
	}
	if txErr.Step == nil {
		t.Fatal("expected Step to be non-nil")
	}
	if txErr.Step.Index != 0 {
		t.Fatalf("expected step index 0, got %d", txErr.Step.Index)
	}
}

func TestTx_RollbacksRunInReverse(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "order.txt")

	tx := NewTx()
	slog := filepath.ToSlash(log)
	tx.AddWithRollback(
		New("true"),
		New("bash", "-c", "echo R0 >> "+slog),
	)
	tx.AddWithRollback(
		New("true"),
		New("bash", "-c", "echo R1 >> "+slog),
	)
	tx.AddWithRollback(
		New("false"), // step 2 fails
		New("bash", "-c", "echo R2 >> "+slog),
	)

	_ = tx.Run()

	data, _ := os.ReadFile(log)
	// Expect reverse order: R2, R1, R0
	got := string(data)
	want := "R2\nR1\nR0\n"
	if got != want {
		t.Fatalf("rollback order: got %q, want %q", got, want)
	}
}

func TestTx_DeferAlwaysRuns(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "defer-ran")

	tx := NewTx()
	tx.Defer(New("touch", marker))
	tx.Add(New("true"))
	_ = tx.Run()

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("defer marker should exist on success")
	}
}

func TestTx_DeferRunsOnFailure(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "defer-ran")

	tx := NewTx()
	tx.Defer(New("touch", marker))
	tx.Add(New("false"))
	_ = tx.Run()

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("defer marker should exist on failure")
	}
}

func TestTx_DeferRunsLIFO(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "order.txt")

	tx := NewTx()
	slog := filepath.ToSlash(log)
	tx.Defer(New("bash", "-c", "echo D0 >> "+slog))
	tx.Defer(New("bash", "-c", "echo D1 >> "+slog))
	tx.Add(New("true"))
	_ = tx.Run()

	data, _ := os.ReadFile(log)
	got := string(data)
	want := "D1\nD0\n"
	if got != want {
		t.Fatalf("defer order: got %q, want %q", got, want)
	}
}

func TestTx_RollbackErrorCollected(t *testing.T) {
	tx := NewTx()
	tx.AddWithRollback(
		New("false"), // fails
		New("false"), // rollback also fails
	)
	err := tx.Run()

	var txErr *TxError
	if !errors.As(err, &txErr) {
		t.Fatalf("expected *TxError, got %T", err)
	}
	if len(txErr.RollbackErrors) != 1 {
		t.Fatalf("expected 1 rollback error, got %d", len(txErr.RollbackErrors))
	}
}

func TestTx_DeferErrorCollected(t *testing.T) {
	tx := NewTx()
	tx.Defer(New("false"))
	tx.Add(New("true"))
	err := tx.Run()

	var txErr *TxError
	if !errors.As(err, &txErr) {
		t.Fatalf("expected *TxError, got %T", err)
	}
	if txErr.Step != nil {
		t.Fatal("Step should be nil when only defers fail")
	}
	if len(txErr.DeferErrors) != 1 {
		t.Fatalf("expected 1 defer error, got %d", len(txErr.DeferErrors))
	}
}

func TestTx_StepOutputCaptured(t *testing.T) {
	tx := NewTx()
	tx.Add(New("bash", "-c", "echo conflict-marker; exit 1"))
	err := tx.Run()

	var txErr *TxError
	if !errors.As(err, &txErr) {
		t.Fatalf("expected *TxError, got %T", err)
	}
	if txErr.Step.Output != "conflict-marker" {
		t.Fatalf("expected output 'conflict-marker', got %q", txErr.Step.Output)
	}
}

func TestTx_UnwrapReturnsStepErr(t *testing.T) {
	tx := NewTx()
	tx.Add(New("false"))
	err := tx.Run()

	var txErr *TxError
	errors.As(err, &txErr)

	unwrapped := txErr.Unwrap()
	if unwrapped == nil {
		t.Fatal("Unwrap should return underlying error")
	}
}

func TestTx_RunContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	tx := NewTx()
	tx.Add(New("sleep", "10"))
	err := tx.RunContext(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestStepError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("exec failed")
	se := &StepError{Err: inner, Output: "some output", Index: 3}
	if got := se.Error(); got != "step 3 failed: exec failed" {
		t.Fatalf("StepError.Error() = %q", got)
	}
	if se.Unwrap() != inner {
		t.Fatal("StepError.Unwrap() should return inner error")
	}
}

func TestTxError_ErrorFormats(t *testing.T) {
	// Step error only.
	te := &TxError{Step: &StepError{Err: errors.New("fail"), Index: 0}}
	if !strings.Contains(te.Error(), "step 0") {
		t.Fatalf("expected step info, got %q", te.Error())
	}

	// Rollback errors only.
	te2 := &TxError{
		Step:           &StepError{Err: errors.New("fail"), Index: 1},
		RollbackErrors: []error{errors.New("rb")},
	}
	if !strings.Contains(te2.Error(), "1 rollback error") {
		t.Fatalf("expected rollback info, got %q", te2.Error())
	}

	// Defer errors only (no step).
	te3 := &TxError{DeferErrors: []error{errors.New("d1"), errors.New("d2")}}
	if !strings.Contains(te3.Error(), "2 defer error") {
		t.Fatalf("expected defer info, got %q", te3.Error())
	}

	// Unwrap with nil Step.
	if te3.Unwrap() != nil {
		t.Fatal("Unwrap should return nil when Step is nil")
	}
}

func TestTx_Empty(t *testing.T) {
	tx := NewTx()
	if err := tx.Run(); err != nil {
		t.Fatalf("empty tx should succeed, got %v", err)
	}
}
