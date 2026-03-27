package ralph

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiBlue   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiGray   = "\x1b[90m"
)

type loopRenderer struct {
	w        io.Writer
	repoRoot string
	pair     SpecPair
}

func newLoopRenderer(w io.Writer, repoRoot string, pair SpecPair) loopRenderer {
	return loopRenderer{w: w, repoRoot: repoRoot, pair: pair}
}

func (r loopRenderer) planning(request string) {
	r.panel("✦", "Ralph planning", []string{
		r.field("Spec", relPath(r.repoRoot, r.pair.SpecPath)),
		r.field("Progress", relPath(r.repoRoot, r.pair.ProgressPath)),
		r.field("Request", summarizeLoopText(request, 88)),
	}, "Updating the durable plan before implementation.")
}

func (r loopRenderer) iteration(iteration, maxIterations int) {
	total := fmt.Sprintf("%d", maxIterations)
	if maxIterations <= 0 {
		total = "∞"
	}
	r.panel("↻", "Ralph builder", []string{
		r.field("Spec", relPath(r.repoRoot, r.pair.SpecPath)),
		r.field("Progress", relPath(r.repoRoot, r.pair.ProgressPath)),
		r.field("Iteration", fmt.Sprintf("%d/%s", iteration, total)),
	}, "Executing the next highest-leverage slice.")
}

func (r loopRenderer) continuing(iteration int) {
	r.status(ansiBlue, "→", fmt.Sprintf("Ralph continuing after iteration %d", iteration), "The builder response requested another pass.")
}

func (r loopRenderer) retry(iteration int, reason string) {
	message := fmt.Sprintf("Ralph will retry after iteration %d", iteration)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "The builder response still needs a valid promise marker."
	}
	r.status(ansiYellow, "↺", message, summarizeLoopText(reason, 120))
}

func (r loopRenderer) completed(iteration int) {
	detail := "The builder response marked this spec as complete."
	if iteration > 0 {
		detail = fmt.Sprintf("Completed in %d iteration", iteration)
		if iteration != 1 {
			detail += "s"
		}
		detail += "."
	}
	r.status(ansiGreen, "✓", "Ralph completed successfully", detail)
}

func (r loopRenderer) interrupted(mode string) {
	label := "Ralph interrupted"
	if mode != "" {
		label = fmt.Sprintf("Ralph %s interrupted", strings.TrimSpace(mode))
	}
	r.status(ansiYellow, "●", label, "No further changes were requested.")
}

func (r loopRenderer) planningReady() {
	r.status(ansiGreen, "✓", "Ralph planning updated the artifacts", fmt.Sprintf("Ready to continue from %s.", relPath(r.repoRoot, r.pair.SpecPath)))
}

func (r loopRenderer) panel(icon, title string, body []string, footer string) {
	if r.w == nil {
		return
	}
	titleLine := strings.TrimSpace(strings.Join([]string{icon, title, loopRendererName(r.pair)}, " "))
	fmt.Fprintf(r.w, "\n╭─ %s\n", colorize(ansiBlue+ansiBold, titleLine))
	for _, line := range body {
		line = strings.TrimRight(line, "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(r.w, "│ %s\n", line)
	}
	if footer != "" {
		fmt.Fprintf(r.w, "╰─ %s\n", colorize(ansiGray, footer))
		return
	}
	fmt.Fprintln(r.w, "╰─")
}

func (r loopRenderer) status(colorCode, icon, title, detail string) {
	if r.w == nil {
		return
	}
	line := strings.TrimSpace(colorize(colorCode+ansiBold, icon) + " " + title)
	if detail == "" {
		fmt.Fprintln(r.w, line)
		return
	}
	fmt.Fprintf(r.w, "%s\n  %s\n", line, colorize(ansiGray, detail))
}

func (r loopRenderer) field(label, value string) string {
	return colorize(ansiGray, fmt.Sprintf("%-9s", label)) + " " + value
}

func loopRendererName(pair SpecPair) string {
	if strings.TrimSpace(pair.Slug) != "" {
		return pair.Slug
	}
	if strings.TrimSpace(pair.SpecPath) == "" {
		return "spec"
	}
	return filepath.Base(pair.SpecPath)
}

func summarizeLoopText(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" {
		return "(none)"
	}
	if limit <= 0 || len(s) <= limit {
		return s
	}
	if limit <= 1 {
		return "…"
	}
	return s[:limit-1] + "…"
}

func colorize(code, text string) string {
	if text == "" {
		return ""
	}
	return code + text + ansiReset
}
