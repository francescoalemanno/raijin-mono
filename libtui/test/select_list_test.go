package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"

	"github.com/stretchr/testify/assert"
)

func TestSelectList_NormalizesMultilineDescriptionsToSingleLine(t *testing.T) {
	testTheme := components.SelectListTheme{
		SelectedPrefix: func(text string) string { return text },
		SelectedText:   func(text string) string { return text },
		Description:    func(text string) string { return text },
		ScrollInfo:     func(text string) string { return text },
		NoMatch:        func(text string) string { return text },
	}

	items := []components.SelectItem{
		{
			Value:       "test",
			Label:       "test",
			Description: "Line one\nLine two\nLine three",
		},
	}

	list := components.NewSelectList(items, 5, testTheme)
	rendered := list.Render(100)

	assert.Greater(t, len(rendered), 0)
	assert.NotContains(t, rendered[0], "\n")
	assert.Contains(t, rendered[0], "Line one Line two Line three")
}
