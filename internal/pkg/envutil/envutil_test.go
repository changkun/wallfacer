package envutil

import (
	"testing"
	"time"
)

func TestInt(t *testing.T) {
	const key = "TEST_ENVUTIL_INT"

	t.Run("default when unset", func(t *testing.T) {
		if got := Int(key, 42); got != 42 {
			t.Errorf("Int() = %d, want 42", got)
		}
	})
	t.Run("default when empty", func(t *testing.T) {
		t.Setenv(key, "")
		if got := Int(key, 42); got != 42 {
			t.Errorf("Int() = %d, want 42", got)
		}
	})
	t.Run("default when unparseable", func(t *testing.T) {
		t.Setenv(key, "abc")
		if got := Int(key, 42); got != 42 {
			t.Errorf("Int() = %d, want 42", got)
		}
	})
	t.Run("parsed value", func(t *testing.T) {
		t.Setenv(key, "7")
		if got := Int(key, 42); got != 7 {
			t.Errorf("Int() = %d, want 7", got)
		}
	})
	t.Run("zero is valid", func(t *testing.T) {
		t.Setenv(key, "0")
		if got := Int(key, 42); got != 0 {
			t.Errorf("Int() = %d, want 0", got)
		}
	})
	t.Run("negative is valid", func(t *testing.T) {
		t.Setenv(key, "-3")
		if got := Int(key, 42); got != -3 {
			t.Errorf("Int() = %d, want -3", got)
		}
	})
}

func TestIntMin(t *testing.T) {
	const key = "TEST_ENVUTIL_INTMIN"

	t.Run("default when unset", func(t *testing.T) {
		if got := IntMin(key, 5, 1); got != 5 {
			t.Errorf("IntMin() = %d, want 5", got)
		}
	})
	t.Run("parsed value above min", func(t *testing.T) {
		t.Setenv(key, "10")
		if got := IntMin(key, 5, 1); got != 10 {
			t.Errorf("IntMin() = %d, want 10", got)
		}
	})
	t.Run("falls back when below min", func(t *testing.T) {
		t.Setenv(key, "0")
		if got := IntMin(key, 5, 1); got != 5 {
			t.Errorf("IntMin() = %d, want 5", got)
		}
	})
	t.Run("exact min is accepted", func(t *testing.T) {
		t.Setenv(key, "1")
		if got := IntMin(key, 5, 1); got != 1 {
			t.Errorf("IntMin() = %d, want 1", got)
		}
	})
}

func TestDuration(t *testing.T) {
	const key = "TEST_ENVUTIL_DUR"

	t.Run("default when unset", func(t *testing.T) {
		if got := Duration(key, time.Hour); got != time.Hour {
			t.Errorf("Duration() = %v, want 1h", got)
		}
	})
	t.Run("default when unparseable", func(t *testing.T) {
		t.Setenv(key, "notaduration")
		if got := Duration(key, time.Hour); got != time.Hour {
			t.Errorf("Duration() = %v, want 1h", got)
		}
	})
	t.Run("parsed value", func(t *testing.T) {
		t.Setenv(key, "30m")
		if got := Duration(key, time.Hour); got != 30*time.Minute {
			t.Errorf("Duration() = %v, want 30m", got)
		}
	})
	t.Run("zero is valid", func(t *testing.T) {
		t.Setenv(key, "0s")
		if got := Duration(key, time.Hour); got != 0 {
			t.Errorf("Duration() = %v, want 0", got)
		}
	})
}

func TestDurationMin(t *testing.T) {
	const key = "TEST_ENVUTIL_DURMIN"

	t.Run("default when unset", func(t *testing.T) {
		if got := DurationMin(key, time.Hour, time.Minute); got != time.Hour {
			t.Errorf("DurationMin() = %v, want 1h", got)
		}
	})
	t.Run("parsed value above min", func(t *testing.T) {
		t.Setenv(key, "2h")
		if got := DurationMin(key, time.Hour, time.Minute); got != 2*time.Hour {
			t.Errorf("DurationMin() = %v, want 2h", got)
		}
	})
	t.Run("falls back when below min", func(t *testing.T) {
		t.Setenv(key, "30s")
		if got := DurationMin(key, time.Hour, time.Minute); got != time.Hour {
			t.Errorf("DurationMin() = %v, want 1h (fallback)", got)
		}
	})
	t.Run("exact min is accepted", func(t *testing.T) {
		t.Setenv(key, "1m")
		if got := DurationMin(key, time.Hour, time.Minute); got != time.Minute {
			t.Errorf("DurationMin() = %v, want 1m", got)
		}
	})
}
