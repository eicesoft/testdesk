package runtimeenv

import (
	"context"
	"os"
	"testing"

	"testdesk/internal/settings"
)

// TestInstallIntegration downloads npm packages and Chromium, so it only runs when
// TESTDESK_INTEGRATION=1 is set.
func TestInstallIntegration(t *testing.T) {
	if os.Getenv("TESTDESK_INTEGRATION") != "1" {
		t.Skip("set TESTDESK_INTEGRATION=1 to run the install test")
	}

	// TESTDESK_RUNTIME_DIR keeps the installed runtime around for the browser tests.
	dir := os.Getenv("TESTDESK_RUNTIME_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	m := New(dir)
	cfg := settings.Defaults()
	// TESTDESK_PYTHON points at a Python install that is not on PATH.
	if py := os.Getenv("TESTDESK_PYTHON"); py != "" {
		cfg.PythonPath = py
	}

	err := m.Install(context.Background(), cfg, func(p Progress) {
		if p.Status != statusLog {
			t.Logf("[%s] %s: %s", p.Step, p.Status, p.Message)
		}
	})
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}

	st := m.Status(context.Background(), cfg)
	if !st.Ready {
		t.Fatalf("runtime not ready after install: %+v", st)
	}
	if _, err := os.Stat(m.Dir() + "/sidecar.mjs"); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
}
