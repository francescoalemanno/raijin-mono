package chat

import "github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"

// listNavigator handles wrap-around up/down keyboard navigation for filterable
// list components. count returns the current number of visible items;
// selected is a pointer to the component's selectedIndex field; update is
// called after every index change so the component can refresh its display.
type listNavigator struct {
	count    func() int
	selected *int
	update   func()
}

// handleNav processes up/down keys and returns true if the key was consumed.
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
	return false
}
