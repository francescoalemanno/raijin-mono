package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/stretchr/testify/assert"
)

func TestFuzzyMatch(t *testing.T) {
	t.Run("empty query matches everything with score 0", func(t *testing.T) {
		result := fuzzy.Match("", "anything")
		assert.True(t, result.Matches)
		assert.Equal(t, 0.0, result.Score)
	})

	t.Run("query longer than text does not match", func(t *testing.T) {
		result := fuzzy.Match("longquery", "short")
		assert.False(t, result.Matches)
	})

	t.Run("exact match has good score", func(t *testing.T) {
		result := fuzzy.Match("test", "test")
		assert.True(t, result.Matches)
		assert.Less(t, result.Score, 0.0) // Should be negative due to consecutive bonuses
	})

	t.Run("characters must appear in order", func(t *testing.T) {
		matchInOrder := fuzzy.Match("abc", "aXbXc")
		assert.True(t, matchInOrder.Matches)

		matchOutOfOrder := fuzzy.Match("abc", "cba")
		assert.False(t, matchOutOfOrder.Matches)
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		result := fuzzy.Match("ABC", "abc")
		assert.True(t, result.Matches)

		result2 := fuzzy.Match("abc", "ABC")
		assert.True(t, result2.Matches)
	})

	t.Run("consecutive matches score better than scattered matches", func(t *testing.T) {
		consecutive := fuzzy.Match("foo", "foobar")
		scattered := fuzzy.Match("foo", "f_o_o_bar")

		assert.True(t, consecutive.Matches)
		assert.True(t, scattered.Matches)
		assert.Less(t, consecutive.Score, scattered.Score)
	})

	t.Run("word boundary matches score better", func(t *testing.T) {
		atBoundary := fuzzy.Match("fb", "foo-bar")
		notAtBoundary := fuzzy.Match("fb", "afbx")

		assert.True(t, atBoundary.Matches)
		assert.True(t, notAtBoundary.Matches)
		assert.Less(t, atBoundary.Score, notAtBoundary.Score)
	})

	t.Run("matches swapped alpha numeric tokens", func(t *testing.T) {
		result := fuzzy.Match("codex52", "gpt-5.2-codex")
		assert.True(t, result.Matches)
	})
}

func TestFuzzyFilter(t *testing.T) {
	t.Run("empty query returns all items unchanged", func(t *testing.T) {
		items := []string{"apple", "banana", "cherry"}
		result := fuzzy.FuzzyFilter(items, "", func(x string) string { return x })
		assert.Equal(t, items, result)
	})

	t.Run("filters out non-matching items", func(t *testing.T) {
		items := []string{"apple", "banana", "cherry"}
		result := fuzzy.FuzzyFilter(items, "an", func(x string) string { return x })
		assert.Contains(t, result, "banana")
		assert.NotContains(t, result, "apple")
		assert.NotContains(t, result, "cherry")
	})

	t.Run("sorts results by match quality", func(t *testing.T) {
		items := []string{"a_p_p", "app", "application"}
		result := fuzzy.FuzzyFilter(items, "app", func(x string) string { return x })

		// "app" should be first (exact consecutive match at start)
		assert.Equal(t, "app", result[0])
	})

	t.Run("works with custom getText function", func(t *testing.T) {
		type Item struct {
			Name string
			ID   int
		}
		items := []Item{
			{Name: "foo", ID: 1},
			{Name: "bar", ID: 2},
			{Name: "foobar", ID: 3},
		}
		result := fuzzy.FuzzyFilter(items, "foo", func(item Item) string { return item.Name })

		assert.Equal(t, 2, len(result))
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = r.Name
		}
		assert.Contains(t, names, "foo")
		assert.Contains(t, names, "foobar")
	})
}
