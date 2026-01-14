package terminal

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
)

// Styles for the selector UI.
var (
	selectorTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15"))

	selectorItemStyle = lipgloss.NewStyle().
				PaddingLeft(2)

	selectorCursorStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Background(lipgloss.Color("236"))

	selectorCheckboxSelected   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("[x]")
	selectorCheckboxUnselected = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[ ]")

	selectorHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8"))

	selectorSummaryStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("7")).
				PaddingLeft(6)

	selectorConfirmStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("11"))
)

// selectorState represents the current state of the selector UI.
type selectorState int

const (
	stateNormal selectorState = iota
	stateConfirmQuit
)

// SelectorModel is the bubbletea model for the interactive finding selector.
type SelectorModel struct {
	findings  []domain.FindingGroup
	selected  map[int]bool // selection state (kept out of domain types)
	expanded  map[int]bool // which items show full details
	cursor    int
	state     selectorState
	confirmed bool
	quitted   bool
}

// NewSelector creates a new selector model with all findings selected by default.
func NewSelector(findings []domain.FindingGroup) SelectorModel {
	selected := make(map[int]bool, len(findings))
	for i := range findings {
		selected[i] = true
	}
	return SelectorModel{
		findings: findings,
		selected: selected,
		expanded: make(map[int]bool),
		cursor:   0,
		state:    stateNormal,
	}
}

// Init implements tea.Model.
func (m SelectorModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m SelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.state == stateConfirmQuit {
		return m.updateConfirmQuit(msg)
	}

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
		case "e":
			m.expanded[m.cursor] = !m.expanded[m.cursor]
		case "a":
			for i := range m.findings {
				m.selected[i] = true
			}
		case "n":
			for i := range m.findings {
				m.selected[i] = false
			}
		case "enter":
			m.confirmed = true
			return m, tea.Quit
		case "q", "esc":
			m.state = stateConfirmQuit
		}
	}
	return m, nil
}

// updateConfirmQuit handles input in the quit confirmation state.
func (m SelectorModel) updateConfirmQuit(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.quitted = true
			return m, tea.Quit
		default:
			// Any other key returns to normal state
			m.state = stateNormal
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m SelectorModel) View() string {
	if len(m.findings) == 0 {
		return "No findings to select.\n"
	}

	var b strings.Builder

	// Header
	b.WriteString(selectorTitleStyle.Render("Select findings to post"))
	b.WriteString("\n\n")

	// Quit confirmation overlay
	if m.state == stateConfirmQuit {
		b.WriteString(selectorConfirmStyle.Render("Skip posting findings? [y/N] "))
		return b.String()
	}

	// Findings list
	for i, finding := range m.findings {
		// Checkbox
		checkbox := selectorCheckboxUnselected
		if m.selected[i] {
			checkbox = selectorCheckboxSelected
		}

		// Title with number and reviewer count
		title := fmt.Sprintf("%s %d. %s", checkbox, i+1, finding.Title)
		if finding.ReviewerCount > 0 {
			title += fmt.Sprintf(" (%d reviewer", finding.ReviewerCount)
			if finding.ReviewerCount > 1 {
				title += "s"
			}
			title += ")"
		}

		// Apply cursor highlighting
		if i == m.cursor {
			b.WriteString(selectorCursorStyle.Render(title))
		} else {
			b.WriteString(selectorItemStyle.Render(title))
		}
		b.WriteString("\n")

		// Show summary if expanded
		if m.expanded[i] && finding.Summary != "" {
			summary := WrapText(finding.Summary, 70, "")
			for _, line := range strings.Split(summary, "\n") {
				b.WriteString(selectorSummaryStyle.Render(line))
				b.WriteString("\n")
			}
		}
	}

	// Footer with help
	b.WriteString("\n")
	help := "↑/↓ navigate • space toggle • e expand • a all • n none • enter confirm • q quit"
	b.WriteString(selectorHelpStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

// SelectedIndices returns the indices of selected findings in sorted order.
func (m SelectorModel) SelectedIndices() []int {
	indices := make([]int, 0, len(m.selected))
	for i, sel := range m.selected {
		if sel {
			indices = append(indices, i)
		}
	}
	sort.Ints(indices)
	return indices
}

// Confirmed returns true if the user confirmed the selection.
func (m SelectorModel) Confirmed() bool {
	return m.confirmed
}

// Quitted returns true if the user quit without confirming.
func (m SelectorModel) Quitted() bool {
	return m.quitted
}

// RunSelector runs the interactive finding selector.
// Returns:
//   - selectedIndices: indices of findings the user selected
//   - canceled: true if user quit without confirming
//   - err: any error from the TUI program
func RunSelector(findings []domain.FindingGroup) (selectedIndices []int, canceled bool, err error) {
	if len(findings) == 0 {
		return nil, false, nil
	}

	model := NewSelector(findings)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return nil, false, err
	}

	m, ok := finalModel.(SelectorModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected model type")
	}

	if m.Quitted() {
		return nil, true, nil
	}

	return m.SelectedIndices(), false, nil
}
