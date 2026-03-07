package chat

import "time"

// renderGate controls rerender cadence using a byte threshold and a time floor.
type renderGate struct {
	minBytes    int
	maxBytes    int
	divisor     int
	minInterval time.Duration

	lastRenderedBytes int
	lastRenderTime    time.Time
	now               func() time.Time
}

func newRenderGate(minBytes, maxBytes, divisor int, minInterval time.Duration) *renderGate {
	if minBytes < 1 {
		minBytes = 1
	}
	if maxBytes < minBytes {
		maxBytes = minBytes
	}
	if divisor < 1 {
		divisor = 1
	}
	return &renderGate{
		minBytes:    minBytes,
		maxBytes:    maxBytes,
		divisor:     divisor,
		minInterval: minInterval,
		now:         time.Now,
	}
}

func (g *renderGate) interval(totalBytes int) int {
	if totalBytes <= 0 {
		return g.minBytes
	}
	interval := min(max(totalBytes/g.divisor, g.minBytes), g.maxBytes)
	return interval
}

func (g *renderGate) shouldRender(totalBytes int, force bool) bool {
	now := g.now()

	if !g.lastRenderTime.IsZero() && now.Sub(g.lastRenderTime) < g.minInterval {
		return false
	}
	if !force && totalBytes-g.lastRenderedBytes < g.interval(totalBytes) {
		return false
	}

	g.lastRenderedBytes = totalBytes
	g.lastRenderTime = now
	return true
}
