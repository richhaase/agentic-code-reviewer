// Package terminal provides terminal output formatting and TTY detection.
package terminal

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
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
	// Placeholder - will be implemented in issue #7
	return m, nil
}

// View implements tea.Model.
func (m SelectorModel) View() string {
	// Placeholder - will be implemented in issue #9
	return ""
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
