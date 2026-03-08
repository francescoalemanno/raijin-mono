package chat

import (
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
)

// listNavigator handles wrap-around keyboard navigation for filterable
// list components. count returns the current number of visible items;
// selected is a pointer to the component's selectedIndex field; update is
// called after every index change so the component can refresh its display.
type listNavigator struct {
	count    func() int
	selected *int
	update   func()
	pageSize int
}

// handleNav processes navigation keys and returns true if the key was consumed.
func (n *listNavigator) handleNav(data string) bool {
	kb := keybindings.GetEditorKeybindings()
	c := n.count()

	if kb.Matches(data, keybindings.ActionSelectUp) {
		if c == 0 {
			return true
		}
		if *n.selected == 0 {
			*n.selected = c - 1
		} else {
			*n.selected--
		}
		n.update()
		return true
	}
	if kb.Matches(data, keybindings.ActionSelectDown) {
		if c == 0 {
			return true
		}
		if *n.selected == c-1 {
			*n.selected = 0
		} else {
			*n.selected++
		}
		n.update()
		return true
	}

	key := keys.ParseKey(data)
	if key == "left" || kb.Matches(data, keybindings.ActionSelectPageUp) {
		n.movePage(-1)
		return true
	}
	if key == "right" || kb.Matches(data, keybindings.ActionSelectPageDown) {
		n.movePage(1)
		return true
	}

	return false
}

func (n *listNavigator) movePage(direction int) {
	c := n.count()
	if c == 0 {
		return
	}

	step := n.pageSize
	if step <= 0 {
		step = 1
	}
	step = min(step, c)

	newIndex := *n.selected + direction*step
	if newIndex < 0 {
		newIndex = c - 1
	}
	if newIndex >= c {
		newIndex = 0
	}
	if newIndex == *n.selected {
		return
	}
	*n.selected = newIndex
	n.update()
}
