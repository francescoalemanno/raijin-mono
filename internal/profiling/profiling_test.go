package profiling

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartStopWritesArtifacts(t *testing.T) {
	rt, err := Start(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !rt.Enabled() {
		t.Fatalf("expected profiling runtime to be enabled")
	}
	if rt.Dir() == "" {
		t.Fatalf("expected profile directory to be set")
	}

	// Burn a small amount of CPU so captured profile has live data.
	deadline := time.Now().Add(20 * time.Millisecond)
	for time.Now().Before(deadline) {
	}

	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	for _, rel := range []string{
		"cpu.pprof",
		"trace.out",
		"heap.pprof",
		"goroutine.pprof",
		"memstats.json",
	} {
		path := filepath.Join(rt.Dir(), rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestStartPprofServer(t *testing.T) {
	rt, err := Start(Options{PprofAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = rt.Stop()
	})

	addr := rt.PprofAddr()
	if addr == "" {
		t.Fatalf("expected non-empty pprof address")
	}

	var (
		resp    *http.Response
		lastErr error
	)
	for range 20 {
		resp, lastErr = http.Get("http://" + addr + "/debug/pprof/")
		if lastErr == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("failed to reach pprof endpoint: %v", lastErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(body), "profiles") {
		t.Fatalf("pprof index response missing expected content")
	}
}
