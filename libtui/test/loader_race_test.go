package test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
)

// mockUI simulates a TUI that dispatches all operations to a single goroutine.
type mockUI struct {
	dispatchCh chan func()
	stopCh     chan struct{}
}

func newMockUI() *mockUI {
	m := &mockUI{
		dispatchCh: make(chan func(), 256),
		stopCh:     make(chan struct{}),
	}
	go m.loop()
	return m
}

func (m *mockUI) loop() {
	for {
		select {
		case fn := <-m.dispatchCh:
			fn()
		case <-m.stopCh:
			return
		}
	}
}

func (m *mockUI) Dispatch(fn func()) {
	select {
	case m.dispatchCh <- fn:
	case <-m.stopCh:
	}
}

func (m *mockUI) RequestRender(force ...bool) {}

func (m *mockUI) Stop() { close(m.stopCh) }

func TestLoader_ConcurrentSetMessageAndLoop_NoRace(t *testing.T) {
	identity := func(s string) string { return s }
	ui := newMockUI()
	defer ui.Stop()

	loader := components.NewLoader(ui, identity, identity, "init")
	t.Cleanup(loader.Stop)

	go loader.Loop()

	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 60 {
				msg := fmt.Sprintf("worker-%d-%d", id, i)
				ui.Dispatch(func() { loader.SetMessage(msg) })
				time.Sleep(2 * time.Millisecond)
			}
		}(worker)
	}

	wg.Wait()
	// Allow at least one spinner tick to run concurrently with message updates.
	time.Sleep(90 * time.Millisecond)
}

func TestLoader_Stop_IsSafeWhenCalledConcurrently(t *testing.T) {
	identity := func(s string) string { return s }
	loader := components.NewLoader(nil, identity, identity, "init")

	go loader.Loop()

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			loader.Stop()
		})
	}
	wg.Wait()
}
