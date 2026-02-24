package test

import (
	"sync"
	"testing"
	"time"

	terminalpkg "github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
)

func TestStdinBuffer_NoStaleTimeoutFlushAfterCompletion(t *testing.T) {
	collector := &testBufferCollector{}
	buffer := terminalpkg.NewStdinBuffer(terminalpkg.StdinBufferOptions{Timeout: 30})
	buffer.SetOnData(collector.OnData)
	buffer.SetOnPaste(collector.OnPaste)

	buffer.Process("\x1b[<35")
	time.Sleep(5 * time.Millisecond)
	buffer.Process(";20;5m")

	// Wait longer than timeout; stale timer callbacks must not flush partial data.
	time.Sleep(45 * time.Millisecond)

	assertSlicesEqual(t, []string{"\x1b[<35;20;5m"}, collector.GetData())
}

func TestStdinBuffer_ConcurrentProcessAndClear_NoRace(t *testing.T) {
	collector := &testBufferCollector{}
	buffer := terminalpkg.NewStdinBuffer(terminalpkg.StdinBufferOptions{Timeout: 5})
	buffer.SetOnData(collector.OnData)
	buffer.SetOnPaste(collector.OnPaste)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 250; i++ {
			buffer.Process("\x1b[<35")
			time.Sleep(150 * time.Microsecond)
			buffer.Process(";20;5m")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 250; i++ {
			time.Sleep(100 * time.Microsecond)
			buffer.Clear()
		}
	}()

	wg.Wait()
	buffer.Destroy()
	time.Sleep(10 * time.Millisecond)
}
