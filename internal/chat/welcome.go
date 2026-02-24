package chat

import (
	"math/rand"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

// welcomeQuotes is a curated list of phrases about storms, lightning, power, and creation.
var welcomeQuotes = []string{
	"Thunder is the sound of giants clapping.",
	"Lightning never strikes twice? Challenge accepted.",
	"In the eye of the storm, find your center.",
	"Electricity is just lightning looking for a home.",
	"The storm clears the air and sharpens the mind.",
	"Some summon rain. I summon solutions.",
	"Code like lightning, refactor like thunder.",
	"Every bolt begins with a single spark of curiosity.",
	"The sky's the limit? Hold my voltage.",
	"Wisdom rides the lightning.",
	"Fork the repo, summon the storm.",
	"Commit often, push harder.",
	"In code we trust, in storms we thrive.",
	"Debugging: where patience meets electricity.",
	"Compile once, run forever.",
	"There are 10 types of people: those who understand binary and those about to learn.",
	"The cloud is just someone else's lightning.",
	"rm -rf fear && mkdir courage",
	"A rolling stone gathers no moss, but a rolling deploy gathers no bugs.",
	"To understand recursion, one must first understand recursion.",
	"sudo make me a sandwich — with extra determination.",
	"May your code be clean and your tests be green.",
	"Tabs or spaces? Lightning doesn't care about indentation.",
	"Zero bugs in production is a myth; zero effort is a choice.",
	"With great power comes great responsibility to document.",
	"The future belongs to those who believe in the beauty of their code.",
	"AI is the lightning; human creativity is the thunder.",
	"Machine learning: teaching silicon to dream in electric patterns.",
	"Every model has a storm inside, waiting to be trained.",
	"Transformers: more than meets the eye.",
	"Neurons fire like lightning across the synapse of imagination.",
	"Prompt engineering: catching lightning in a bottle.",
	"The singularity is just a really impressive thunderclap.",
	"Large language models: because one mind is never enough.",
	"From random noise to meaning: that's the real magic.",
	"Fine-tuning is the art of sculpting storms.",
	"Bottlenecks are just thunderstorms in disguise.",
	"Overfitting is when your model chases shadows instead of lightning.",
	"Gradient descent: rolling downhill through valleys of loss.",
	"Attention is all you need... and maybe a good GPU.",
	"Weights and biases: the yin and yang of neural networks.",
	"Backpropagation: learning from every strike of insight.",
	"The cloud isn't just for storage; it's where ideas spark.",
	"GPT: Great Potential for Thunder.",
	"Inference is where the lightning actually hits.",
	"Context windows: the eye of the neural storm.",
	"Hallucinations: when AI dreams of electric sheep.",
	"The only thing more powerful than a bolt from the blue? A well-timed query.",
	"Temperature zero: deterministic lightning. Temperature one: creative chaos.",
	"Vector spaces: where meaning finds its coordinates in the storm.",
	"RAG: when you need your lightning grounded in truth.",
	"Tokens flow like electricity through the transformer grid.",
	"Multi-head attention: many storms, one sky.",
	"Embeddings: capturing the essence of thought in vectors.",
	"The epoch ends, but the learning continues.",
}

// asciiArtLogo is the Raijin ASCII banner.
const asciiArtLogo = `
██████╗   █████╗  ██╗    ████╗ ██╗ ███╗   ██╗
██╔══██╗ ██╔══██╗ ██║      ██║ ██║ ████╗  ██║
██████╔╝ ███████║ ██║      ██║ ██║ ██╔██╗ ██║
██╔══██╗ ██╔══██║ ██║ ██   ██║ ██║ ██║╚██╗██║
██║  ██║ ██║  ██║ ██║ ███████║ ██║ ██║ ╚████║
╚═╝  ╚═╝ ╚═╝  ╚═╝ ╚═╝ ╚══════╝ ╚═╝ ╚═╝  ╚═══╝
`

// WelcomeComponent renders the initial welcome screen for Raijin.
type WelcomeComponent struct {
	cachedLines []string
	cachedWidth int
	skills      []string
	tools       []string
}

// NewWelcomeComponent creates a new welcome component with a random quote.
func NewWelcomeComponent(skills, tools []string) *WelcomeComponent {
	return &WelcomeComponent{
		skills: sanitizeNames(skills),
		tools:  sanitizeNames(tools),
	}
}

// Render generates the welcome screen content.
func (w *WelcomeComponent) Render(width int) []string {
	if w.cachedLines != nil && w.cachedWidth == width {
		return w.cachedLines
	}

	// Ensure width is at least 1
	if width < 1 {
		width = 1
	}

	contentWidth := width - 2 // Account for borders
	if contentWidth < 20 {
		contentWidth = 20
	}

	// truncLine ensures a line never exceeds the terminal width
	truncLine := func(line string) string {
		return utils.TruncateToWidth(line, width, "")
	}

	var lines []string

	// Top border
	lines = append(lines, truncLine(w.centerLine("┌"+strings.Repeat("─", max(0, width-2))+"┐", width)))

	// ASCII logo
	logoLines := strings.Split(asciiArtLogo, "\n")
	for _, line := range logoLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, truncLine(w.centerLine(theme.ColorAccent(line), width)))
		}
	}

	// Separator
	lines = append(lines, truncLine(w.centerLine(theme.ColorMuted("─"+strings.Repeat("─", max(0, contentWidth-2))+"─"), width)))

	// Random quote
	quote := welcomeQuotes[rand.Intn(len(welcomeQuotes))]
	quoteLines := utils.WrapTextWithAnsi(theme.ColorMuted("✦ ")+theme.ColorAccentAlt(quote), max(1, contentWidth-4))
	for _, line := range quoteLines {
		lines = append(lines, truncLine(w.padLine("  "+line, width)))
	}

	// Separator
	lines = append(lines, truncLine(w.centerLine(theme.ColorMuted("─"+strings.Repeat("─", max(0, contentWidth-2))+"─"), width)))

	// Quick Start section
	lines = append(lines, truncLine(w.centerLine(theme.ColorAccentAltBold("QUICK START"), width)))
	lines = append(lines, "")

	quickStart := []string{
		"Type your message and press " + theme.ColorAccent("Enter") + " to chat",
		"Press " + theme.ColorAccent("Ctrl+P") + " to switch models",
		"Press " + theme.ColorAccent("Ctrl+O") + " to expand/collapse tool blocks",
		"Type " + theme.ColorAccent("/help") + " for all commands",
	}
	for _, tip := range quickStart {
		lines = append(lines, truncLine(w.padLine("  "+tip, width)))
	}

	// Separator
	lines = append(lines, "")
	lines = append(lines, truncLine(w.centerLine(theme.ColorMuted("─"+strings.Repeat("─", max(0, contentWidth-2))+"─"), width)))

	// Commands section
	lines = append(lines, truncLine(w.centerLine(theme.ColorAccentAltBold("COMMANDS"), width)))
	lines = append(lines, "")

	var commands []commandHelp
	for _, c := range commandNamesDescs {
		if strings.Contains(c.Command, " ") {
			continue
		}
		commands = append(commands, c)
	}

	for _, c := range commands {
		cmdStr := theme.ColorAccent(c.Command)
		descStr := theme.ColorMuted(c.Desc)
		fullLine := "  " + cmdStr + strings.Repeat(" ", 12-len(c.Command)) + descStr
		lines = append(lines, truncLine(w.padLine(fullLine, width)))
	}

	if len(w.tools) > 0 || len(w.skills) > 0 {
		// Separator
		lines = append(lines, "")
		lines = append(lines, truncLine(w.centerLine(theme.ColorMuted("─"+strings.Repeat("─", max(0, contentWidth-2))+"─"), width)))

		// Loaded runtime section
		lines = append(lines, truncLine(w.centerLine(theme.ColorAccentAltBold("LOADED"), width)))
		lines = append(lines, "")
		if len(w.tools) > 0 {
			lines = append(lines, truncLine(w.padLine("  "+theme.ColorAccent("Custom tools")+theme.ColorMuted(" ("+countLabel(len(w.tools))+")"), width)))
			for _, line := range w.renderNameList(w.tools, contentWidth-4) {
				lines = append(lines, truncLine(w.padLine("    "+line, width)))
			}
		}
		if len(w.tools) > 0 && len(w.skills) > 0 {
			lines = append(lines, "")
		}
		if len(w.skills) > 0 {
			lines = append(lines, truncLine(w.padLine("  "+theme.ColorAccent("Custom skills")+theme.ColorMuted(" ("+countLabel(len(w.skills))+")"), width)))
			for _, line := range w.renderNameList(w.skills, contentWidth-4) {
				lines = append(lines, truncLine(w.padLine("    "+line, width)))
			}
		}
	}

	// Bottom border
	lines = append(lines, truncLine(w.centerLine("└"+strings.Repeat("─", max(0, width-2))+"┘", width)))

	w.cachedLines = lines
	w.cachedWidth = width
	return lines
}

// centerLine centers text within the given width.
func (w *WelcomeComponent) centerLine(text string, width int) string {
	visibleWidth := utils.VisibleWidth(text)
	if visibleWidth >= width {
		return text
	}
	padding := (width - visibleWidth) / 2
	return strings.Repeat(" ", padding) + text
}

// padLine pads a line to fit the width.
func (w *WelcomeComponent) padLine(text string, width int) string {
	visibleWidth := utils.VisibleWidth(text)
	if visibleWidth >= width {
		return text
	}
	return text + strings.Repeat(" ", width-visibleWidth)
}

func (w *WelcomeComponent) renderNameList(names []string, width int) []string {
	return utils.WrapTextWithAnsi(strings.Join(names, theme.ColorMuted(", ")), max(1, width))
}

func sanitizeNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return slices.Clip(out)
}

func countLabel(count int) string {
	return strconv.Itoa(count)
}

func (w *WelcomeComponent) HandleInput(data string) {}

func (w *WelcomeComponent) Invalidate() {
	w.cachedLines = nil
	w.cachedWidth = 0
}

var _ tui.Component = (*WelcomeComponent)(nil)

// ShowWelcomeMessage adds the welcome component to the app's history.
func (app *ChatApp) ShowWelcomeMessage() {
	welcome := app.newWelcomeComponent()
	app.history.AddChild(welcome)
	app.items = append(app.items, historyEntry{component: welcome})
	app.ui.RequestRender()
}
