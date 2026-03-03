package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestTruncateToWidth_FitsReturnsInput(t *testing.T) {
	in := "\x1b[32mhello\x1b[0m"
	out := utils.TruncateToWidth(in, 10)
	assert.Equal(t, in, out)
}

func TestTruncateToWidth_TruncatesAnsiAndAppendsReset(t *testing.T) {
	in := "\x1b[31mhello world\x1b[0m"
	out := utils.TruncateToWidth(in, 6)

	assert.LessOrEqual(t, utils.VisibleWidth(out), 6)
	assert.Contains(t, out, "\x1b[0m...")
}

func TestTruncateToWidth_RespectsTabWidth(t *testing.T) {
	in := "a\tbcd"
	out := utils.TruncateToWidth(in, 4, "")

	assert.LessOrEqual(t, utils.VisibleWidth(out), 4)
	assert.Contains(t, out, "a\t")
}

func TestTruncateToWidth_ZeroWidth(t *testing.T) {
	assert.Equal(t, "", utils.TruncateToWidth("hello", 0))
}
