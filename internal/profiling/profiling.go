package profiling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	runtimepprof "runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

// Options controls runtime profiling behavior.
type Options struct {
	// Dir writes captured profiles to a timestamped subdirectory.
	Dir string
	// PprofAddr starts an HTTP pprof endpoint on this address.
	// Example: "127.0.0.1:6060".
	PprofAddr string
}

// Runtime owns active profiling resources.
type Runtime struct {
	dir string

	cpuFile      *os.File
	cpuOn        bool
	traceFile    *os.File
	traceOn      bool
	restoreMutex int

	server   *http.Server
	listener net.Listener

	mu      sync.Mutex
	stopped bool
}

// Start initializes profiling according to opts.
func Start(opts Options) (*Runtime, error) {
	rt := &Runtime{}

	if opts.Dir == "" && opts.PprofAddr == "" {
		return rt, nil
	}

	if opts.Dir != "" {
		baseDir, err := filepath.Abs(opts.Dir)
		if err != nil {
			return nil, fmt.Errorf("resolve profile dir: %w", err)
		}
		sessionDir := filepath.Join(baseDir, "raijin-profile-"+time.Now().Format("20060102-150405"))
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			return nil, fmt.Errorf("create profile dir %q: %w", sessionDir, err)
		}
		rt.dir = sessionDir

		// Enable contention profiles while session is active.
		runtime.SetBlockProfileRate(1)
		rt.restoreMutex = runtime.SetMutexProfileFraction(5)

		cpuFile, err := os.Create(filepath.Join(sessionDir, "cpu.pprof"))
		if err != nil {
			rt.stopCaptureBestEffort()
			return nil, fmt.Errorf("create cpu profile: %w", err)
		}
		rt.cpuFile = cpuFile
		if err := runtimepprof.StartCPUProfile(cpuFile); err != nil {
			rt.stopCaptureBestEffort()
			return nil, fmt.Errorf("start cpu profile: %w", err)
		}
		rt.cpuOn = true

		traceFile, err := os.Create(filepath.Join(sessionDir, "trace.out"))
		if err != nil {
			rt.stopCaptureBestEffort()
			return nil, fmt.Errorf("create trace output: %w", err)
		}
		rt.traceFile = traceFile
		if err := trace.Start(traceFile); err != nil {
			rt.stopCaptureBestEffort()
			return nil, fmt.Errorf("start runtime trace: %w", err)
		}
		rt.traceOn = true
	}

	if opts.PprofAddr != "" {
		ln, err := net.Listen("tcp", opts.PprofAddr)
		if err != nil {
			rt.stopCaptureBestEffort()
			return nil, fmt.Errorf("listen on pprof addr %q: %w", opts.PprofAddr, err)
		}
		rt.listener = ln
		rt.server = &http.Server{}

		go func() {
			_ = rt.server.Serve(ln)
		}()
	}

	return rt, nil
}

// Enabled reports whether any profiling mode is active.
func (rt *Runtime) Enabled() bool {
	return rt != nil && (rt.cpuOn || rt.traceOn || rt.listener != nil)
}

// Dir returns the active profile output directory, if enabled.
func (rt *Runtime) Dir() string {
	if rt == nil {
		return ""
	}
	return rt.dir
}

// PprofAddr returns the active pprof listen address, if enabled.
func (rt *Runtime) PprofAddr() string {
	if rt == nil || rt.listener == nil {
		return ""
	}
	return rt.listener.Addr().String()
}

// Stop flushes and closes profiling resources.
func (rt *Runtime) Stop() error {
	if rt == nil {
		return nil
	}

	rt.mu.Lock()
	if rt.stopped {
		rt.mu.Unlock()
		return nil
	}
	rt.stopped = true
	rt.mu.Unlock()

	var errs []error

	if rt.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := rt.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown pprof server: %w", err))
		}
		cancel()
	}
	if rt.listener != nil {
		if err := rt.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("close pprof listener: %w", err))
		}
	}

	if rt.traceOn {
		trace.Stop()
		if rt.traceFile != nil {
			if err := rt.traceFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close trace output: %w", err))
			}
		}
	}
	if rt.cpuOn {
		runtimepprof.StopCPUProfile()
		if rt.cpuFile != nil {
			if err := rt.cpuFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close cpu profile: %w", err))
			}
		}
	}
	if rt.cpuOn || rt.traceOn {
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(rt.restoreMutex)
		if err := rt.writeSnapshotProfiles(); err != nil {
			errs = append(errs, err)
		}
		if err := rt.writeMemStats(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (rt *Runtime) writeSnapshotProfiles() error {
	if rt.dir == "" {
		return nil
	}

	var errs []error
	for _, item := range []struct {
		name  string
		file  string
		debug int
	}{
		{name: "heap", file: "heap.pprof"},
		{name: "goroutine", file: "goroutine.pprof"},
		{name: "block", file: "block.pprof"},
		{name: "mutex", file: "mutex.pprof"},
	} {
		p := runtimepprof.Lookup(item.name)
		if p == nil {
			continue
		}
		f, err := os.Create(filepath.Join(rt.dir, item.file))
		if err != nil {
			errs = append(errs, fmt.Errorf("create %s: %w", item.file, err))
			continue
		}
		if err := p.WriteTo(f, item.debug); err != nil {
			errs = append(errs, fmt.Errorf("write %s: %w", item.file, err))
		}
		if err := f.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", item.file, err))
		}
	}
	return errors.Join(errs...)
}

func (rt *Runtime) writeMemStats() error {
	if rt.dir == "" {
		return nil
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	f, err := os.Create(filepath.Join(rt.dir, "memstats.json"))
	if err != nil {
		return fmt.Errorf("create memstats.json: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ms); err != nil {
		return fmt.Errorf("write memstats.json: %w", err)
	}
	return nil
}

func (rt *Runtime) stopCaptureBestEffort() {
	if rt == nil {
		return
	}
	if rt.traceOn {
		trace.Stop()
	}
	if rt.cpuOn {
		runtimepprof.StopCPUProfile()
	}
	runtime.SetBlockProfileRate(0)
	runtime.SetMutexProfileFraction(rt.restoreMutex)
	if rt.traceFile != nil {
		_ = rt.traceFile.Close()
	}
	if rt.cpuFile != nil {
		_ = rt.cpuFile.Close()
	}
	if rt.listener != nil {
		_ = rt.listener.Close()
	}
	if rt.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = rt.server.Shutdown(ctx)
		cancel()
	}
}
