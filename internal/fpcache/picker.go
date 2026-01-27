package fpcache

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// Styles for the picker UI.
var (
	pickerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15"))

	pickerItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	pickerCursorStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Background(lipgloss.Color("236"))

	pickerCheckboxSelected   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("[x]")
	pickerCheckboxUnselected = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[ ]")

	pickerHelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	pickerSummaryStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("7")).
				PaddingLeft(6)
)

// PickerModel is the bubbletea model for the false positive picker.
type PickerModel struct {
	findings  []LastRunFinding
	selected  map[int]bool // selection state (true = mark as FP)
	cursor    int
	confirmed bool
	quitted   bool
}

// NewPicker creates a new picker model.
// Pre-selects findings that match patterns in alreadyIgnored.
func NewPicker(findings []LastRunFinding, alreadyIgnored []string) PickerModel {
	selected := make(map[int]bool, len(findings))

	// Pre-check findings that are already in the ignore file
	for i, finding := range findings {
		if MatchesIgnore(finding.Title, alreadyIgnored) {
			selected[i] = true
		}
	}

	return PickerModel{
		findings:  findings,
		selected:  selected,
		cursor:    0,
		confirmed: false,
		quitted:   false,
	}
}

// Init implements tea.Model.
func (m PickerModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.findings)-1 {
				m.cursor++
			}
		case " ":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "enter":
			m.confirmed = true
			return m, tea.Quit
		case "q", "esc":
			m.quitted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m PickerModel) View() string {
	if len(m.findings) == 0 {
		return "No findings to mark.\n"
	}

	var b strings.Builder

	// Header
	b.WriteString(pickerTitleStyle.Render("Mark findings as false positives"))
	b.WriteString("\n\n")

	// Findings list
	for i, finding := range m.findings {
		// Checkbox
		checkbox := pickerCheckboxUnselected
		if m.selected[i] {
			checkbox = pickerCheckboxSelected
		}

		// Title
		title := fmt.Sprintf("%s %d. %s", checkbox, i+1, finding.Title)

		// Apply cursor highlighting
		if i == m.cursor {
			b.WriteString(pickerCursorStyle.Render(title))
		} else {
			b.WriteString(pickerItemStyle.Render(title))
		}
		b.WriteString("\n")

		// Show truncated summary (first 100 chars)
		if finding.Summary != "" {
			summary := finding.Summary
			if len(summary) > 100 {
				summary = summary[:97] + "..."
			}
			// Wrap and indent
			wrapped := terminal.WrapText(summary, 70, "")
			for _, line := range strings.Split(wrapped, "\n") {
				b.WriteString(pickerSummaryStyle.Render(line))
				b.WriteString("\n")
			}
		}
	}

	// Footer with help
	b.WriteString("\n")
	help := "↑/↓ navigate • space toggle • enter save • q quit"
	b.WriteString(pickerHelpStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

// SelectedTitles returns the titles of selected findings in the order they appear.
func (m PickerModel) SelectedTitles() []string {
	titles := make([]string, 0, len(m.selected))
	// Iterate in index order for consistent output
	indices := make([]int, 0, len(m.selected))
	for i, sel := range m.selected {
		if sel {
			indices = append(indices, i)
		}
	}
	slices.Sort(indices)

	for _, i := range indices {
		titles = append(titles, m.findings[i].Title)
	}
	return titles
}

// Confirmed returns true if the user confirmed the selection.
func (m PickerModel) Confirmed() bool {
	return m.confirmed
}

// Quitted returns true if the user quit without confirming.
func (m PickerModel) Quitted() bool {
	return m.quitted
}

// RunPicker runs the interactive false positive picker.
// Returns:
//   - selected: titles of findings marked as false positives
//   - nil if user canceled (q/Esc)
//   - error if not a TTY or TUI program fails
func RunPicker(findings []LastRunFinding, alreadyIgnored []string) ([]string, error) {
	// Check if stdin is a TTY
	if !terminal.IsStdinTTY() {
		return nil, fmt.Errorf("mark-fp requires an interactive terminal (not a TTY)")
	}

	if len(findings) == 0 {
		return []string{}, nil
	}

	model := NewPicker(findings, alreadyIgnored)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("picker UI error: %w", err)
	}

	m, ok := finalModel.(PickerModel)
	if !ok {
		return nil, fmt.Errorf("unexpected model type")
	}

	// Return nil if user quit without confirming
	if m.Quitted() {
		return nil, nil
	}

	return m.SelectedTitles(), nil
}
