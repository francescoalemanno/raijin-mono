package chat

import (
	"testing"

	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

func TestGlobalKeyListener_ModalShortcutClosesModalWithoutInterrupt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
	}{
		{name: "ctrl+c", input: "\x03"},
		{name: "escape", input: "\x1b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

			var (
				res      *tui.InputListenerResult
				queueLen int
			)
			app.dispatchSync(func(_ tui.UIToken) {
				app.state = stateRunning
				app.steeringQueue = newPromptDeliveryQueue()
				app.steeringQueue.Enqueue(queuedPrompt{Input: "stay", Opts: promptRunOptions{}})
				app.showSelector(func(done func()) tui.Component {
					return &expandableTestComponent{}
				})
				if app.activeModalDone == nil {
					t.Fatalf("expected active modal callback")
				}

				res = app.globalKeyListener(tc.input)
				queueLen = app.steeringQueue.Len()
			})

			if res == nil || !res.Consume {
				t.Fatalf("expected %s to be consumed while modal is open", tc.name)
			}

			app.dispatchSync(func(_ tui.UIToken) {
				if app.activeModalDone != nil {
					t.Fatalf("expected modal to be closed")
				}
			})
			if queueLen != 1 {
				t.Fatalf("steering queue len = %d, want 1 (no interrupt)", queueLen)
			}
		})
	}
}

func TestShowSelector_StaleDoneCannotCloseNewModal(t *testing.T) {
	t.Parallel()

	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

	var (
		firstDone  func()
		secondDone func()
	)

	app.dispatchSync(func(_ tui.UIToken) {
		app.showSelector(func(done func()) tui.Component {
			firstDone = done
			return &expandableTestComponent{}
		})
		if firstDone == nil {
			t.Fatalf("expected first done callback")
		}
		firstID := app.activeModalID
		if firstID == 0 {
			t.Fatalf("expected first modal id")
		}

		app.showSelector(func(done func()) tui.Component {
			secondDone = done
			return &expandableTestComponent{}
		})
		if secondDone == nil {
			t.Fatalf("expected second done callback")
		}
		secondID := app.activeModalID
		if secondID == 0 || secondID == firstID {
			t.Fatalf("expected new active modal id, got first=%d second=%d", firstID, secondID)
		}

		firstDone()
		if app.activeModalID != secondID {
			t.Fatalf("stale done closed active modal: active id=%d want %d", app.activeModalID, secondID)
		}
		if app.activeModalDone == nil {
			t.Fatalf("stale done cleared active modal callback")
		}

		secondDone()
		if app.activeModalID != 0 {
			t.Fatalf("expected modal closed, active id=%d", app.activeModalID)
		}
		if app.activeModalDone != nil {
			t.Fatalf("expected modal callback cleared")
		}
	})
}
