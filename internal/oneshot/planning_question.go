package oneshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/francescoalemanno/raijin-mono/internal/ralph"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
)

var errPlanningQuestionInteractiveRequired = errors.New("interactive clarification required: Ralph planning question needs a TTY")

const planningQuestionOtherKey = "__other__"

var (
	pickPlanningQuestionChoice = defaultPickPlanningQuestionChoice
	readPlanningInlineAnswer   = runPlanningInlineAnswer
)

func runPlanningQuestionPrompt(ctx context.Context, prompt ralph.PlanningQuestionPrompt) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	question := strings.TrimSpace(prompt.Question)
	if question == "" {
		return "", errors.New("planning question is empty")
	}

	items := make([]fzfPickerItem, 0, len(prompt.Options)+1)
	keyToAnswer := make(map[string]string, len(prompt.Options)+1)
	for idx, option := range prompt.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			return "", errors.New("planning question option label is empty")
		}
		key := fmt.Sprintf("option-%d", idx)
		preview := strings.TrimSpace(option.Description)
		if preview == "" {
			preview = "Select this answer."
		}
		items = append(items, fzfPickerItem{
			key:     key,
			label:   label,
			preview: preview,
		})
		keyToAnswer[key] = label
	}
	var chosenKey string
	err := withInteractiveQuestionDialog(func() error {
		var err error
		chosenKey, err = pickPlanningQuestionChoice(question, items)
		return err
	})
	if err != nil {
		return "", err
	}
	if chosenKey == planningQuestionOtherKey {
		var answer string
		err := withInteractiveQuestionDialog(func() error {
			var err error
			answer, err = readPlanningInlineAnswer(question)
			return err
		})
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(answer), nil
	}

	answer, ok := keyToAnswer[chosenKey]
	if !ok {
		return "", fmt.Errorf("unknown planning question selection: %s", chosenKey)
	}
	return answer, nil
}

func defaultPickPlanningQuestionChoice(question string, items []fzfPickerItem) (string, error) {
	if !canUseEmbeddedFZFForTest() {
		return "", errPlanningQuestionInteractiveRequired
	}
	items = append(items, fzfPickerItem{
		key:     planningQuestionOtherKey,
		label:   "Other",
		preview: "Provide a free-form answer inline.",
	})
	chosenKey, action, err := pickWithEmbeddedFZFConfig(items, "", false, true, "", "default", shellinit.RunFZFOptions{
		Header:        question,
		UseFullscreen: true,
	})
	if err != nil {
		if errors.Is(err, errFZFPickerUnavailable) {
			return "", errPlanningQuestionInteractiveRequired
		}
		return "", err
	}
	if action != fzfPickerActionSelect {
		return "", context.Canceled
	}
	return chosenKey, nil
}

func withInteractiveQuestionDialog(fn func() error) error {
	if fn == nil {
		return nil
	}
	endDialog := beginCurrentRendererInteractiveDialog()
	defer endDialog()
	return fn()
}

type planningInlineAnswerModel struct {
	question  string
	editor    textarea.Model
	width     int
	result    string
	submitted bool
	canceled  bool
}

func runPlanningInlineAnswer(question string) (string, error) {
	model := planningInlineAnswerModel{
		question: strings.TrimSpace(question),
		editor:   newREPLEditor(),
	}
	model.editor.Placeholder = "Type an answer and press Enter. Alt+Enter inserts a newline."

	program := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	finalModel, err := program.Run()
	if err != nil {
		return "", err
	}
	result, ok := finalModel.(planningInlineAnswerModel)
	if !ok {
		return "", errors.New("unexpected planning inline answer result")
	}
	if result.canceled {
		return "", context.Canceled
	}
	if !result.submitted {
		return "", context.Canceled
	}
	return strings.TrimSpace(result.result), nil
}

func (m planningInlineAnswerModel) Init() tea.Cmd {
	return nil
}

func (m planningInlineAnswerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			answer := strings.TrimSpace(m.editor.Value())
			if answer == "" {
				return m, nil
			}
			m.result = answer
			m.submitted = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m planningInlineAnswerModel) View() tea.View {
	var b strings.Builder
	b.WriteString(RenderThemedInfo("?"))
	b.WriteString(" ")
	b.WriteString(m.question)
	b.WriteString("\n")
	b.WriteString(RenderThemedDim("Choose Enter to submit, Alt+Enter for newline, Esc to cancel."))
	b.WriteString("\n\n")
	b.WriteString(renderPlanningInlineEditor(m.editor, m.width))
	return tea.NewView(b.String())
}

func renderPlanningInlineEditor(editor textarea.Model, width int) string {
	value := editor.Value()
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	cursorLine := editor.Line()
	cursorColumn := editor.Column()

	promptWidth := len([]rune(replPrompt))
	availableWidth := width - promptWidth
	if availableWidth <= 0 {
		availableWidth = 80
	}

	var b strings.Builder
	for i, logicalLine := range lines {
		runes := []rune(logicalLine)
		if len(runes) == 0 {
			prompt := replContinuationPrompt
			if i == 0 {
				prompt = replPrompt
			}
			b.WriteString(prompt)
			if i == cursorLine {
				b.WriteString(renderBufferWithCursor(nil, 0))
			}
			if i < len(lines)-1 {
				b.WriteByte('\n')
			}
			continue
		}

		for start := 0; start < len(runes); start += availableWidth {
			end := min(start+availableWidth, len(runes))

			prompt := replContinuationPrompt
			if i == 0 && start == 0 {
				prompt = replPrompt
			}
			b.WriteString(prompt)

			if i == cursorLine && cursorColumn >= start && (cursorColumn < end || (cursorColumn == end && end == len(runes))) {
				b.WriteString(renderBufferWithCursor(runes[start:end], cursorColumn-start))
			} else {
				b.WriteString(string(runes[start:end]))
			}

			if end < len(runes) {
				b.WriteByte('\n')
			}
		}

		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
