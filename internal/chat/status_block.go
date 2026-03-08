package chat

import (
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

type StatusState int

const (
	StatusPending StatusState = iota
	StatusSuccess
	StatusError
)

type StatusBlock struct {
	ui components.UILike

	box     *components.Box
	content *components.Text
	loader  *components.Loader

	state    StatusState
	expanded bool
	text     string

	cachedWidth int
	cachedLines []string
}

func NewStatusBlock(ui components.UILike, loaderPrimary, loaderSecondary func(string) string, loaderLabel string) *StatusBlock {
	s := &StatusBlock{ui: ui}
	s.box = components.NewBox(1, 0, theme.Default.BgToolPending.AnsiBgOnly)
	s.content = components.NewText("", 0, 0, nil)
	s.box.AddChild(s.content)

	s.loader = components.NewLoader(ui, loaderPrimary, loaderSecondary, loaderLabel)
	s.box.AddChild(s.loader)
	go s.loader.Loop()

	return s
}

func (s *StatusBlock) SetText(text string) {
	if s.text == text {
		return
	}
	s.text = text
	s.content.SetText(text)
	s.invalidateCache()
}

// Transition atomically updates both text and state, ensuring no intermediate
// render can observe a mismatched background/text combination.
func (s *StatusBlock) Transition(st StatusState, text string) {
	textChanged := s.text != text
	if textChanged {
		s.text = text
		s.content.SetText(text)
	}

	stateChanged := s.state != st
	if stateChanged {
		s.state = st
		s.applyState()
	}

	if textChanged || stateChanged {
		s.invalidateCache()
	}
}

func (s *StatusBlock) applyState() {
	if s.state != StatusPending && s.loader != nil {
		s.loader.Stop()
		s.box.RemoveChild(s.loader)
		s.loader = nil
	}

	switch s.state {
	case StatusPending:
		s.box.SetBgFn(theme.Default.BgToolPending.AnsiBgOnly)
	case StatusError:
		s.box.SetBgFn(theme.Default.BgToolError.AnsiBgOnly)
	default:
		s.box.SetBgFn(theme.Default.BgToolSuccess.AnsiBgOnly)
	}
}

func (s *StatusBlock) State() StatusState { return s.state }

func (s *StatusBlock) SetExpanded(expanded bool) {
	s.expanded = expanded
}

func (s *StatusBlock) IsExpanded() bool { return s.expanded }

func (s *StatusBlock) Render(width int) []string {
	if s.state != StatusPending {
		if s.cachedLines != nil && s.cachedWidth == width {
			return s.cachedLines
		}
		lines := s.box.Render(width)
		s.cachedWidth = width
		s.cachedLines = lines
		return lines
	}
	return s.box.Render(width)
}

func (s *StatusBlock) HandleInput(data string) {}

func (s *StatusBlock) Invalidate() {
	s.invalidateCache()
	s.box.Invalidate()
}

func (s *StatusBlock) invalidateCache() {
	s.cachedWidth = 0
	s.cachedLines = nil
}

var _ tui.Component = (*StatusBlock)(nil)
