package components

import (
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

// NewCancellableLoader creates a new CancellableLoader component.
func NewCancellableLoader(ui UILike, spinnerColorFn, messageColorFn func(string) string, message string) *CancellableLoader {
	l := NewLoader(ui, spinnerColorFn, messageColorFn, message)
	return &CancellableLoader{
		Loader: l,
	}
}

// CancellableLoader extends Loader with abort capability.
// User can press Escape to cancel the async operation.
type CancellableLoader struct {
	*Loader
	abortController *abortController
}

// abortController is a simple implementation of an abort signal.
type abortController struct {
	sig *abortSignal
}

type abortSignal struct {
	aborted bool
}

func newAbortController() *abortController {
	return &abortController{
		sig: &abortSignal{},
	}
}

func (a *abortController) abort() {
	a.sig.aborted = true
}

// Signal returns the abort signal.
func (c *CancellableLoader) Signal() *abortSignal {
	if c.abortController == nil {
		c.abortController = newAbortController()
	}
	return c.abortController.sig
}

// Aborted returns whether the loader was aborted.
func (c *CancellableLoader) Aborted() bool {
	if c.abortController == nil {
		return false
	}
	return c.abortController.sig.aborted
}

// HandleInput checks for Escape key to abort.
func (c *CancellableLoader) HandleInput(data string) {
	kb := keybindings.GetEditorKeybindings()
	if kb.Matches(data, keybindings.ActionSelectCancel) {
		if c.abortController == nil {
			c.abortController = newAbortController()
		}
		c.abortController.abort()
	}
}

// Dispose stops the loader and cleans up.
func (c *CancellableLoader) Dispose() {
	c.Stop()
}

// Ensure CancellableLoader implements Component interface.
var _ tui.Component = (*CancellableLoader)(nil)
