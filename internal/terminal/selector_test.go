package terminal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestNewSelector_AllSelectedByDefault(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}

	m := NewSelector(findings)

	for i := range findings {
		if !m.selected[i] {
			t.Errorf("expected finding %d to be selected by default", i)
		}
	}
}

func TestNewSelector_EmptyFindings(t *testing.T) {
	m := NewSelector(nil)

	if len(m.findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(m.findings))
	}
	if m.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", m.cursor)
	}
}

func TestSelectedIndices_ReturnsCorrectIndices(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}

	m := NewSelector(findings)
	m.selected[1] = false // Deselect middle finding

	indices := m.SelectedIndices()

	if len(indices) != 2 {
		t.Fatalf("expected 2 indices, got %d", len(indices))
	}
	if indices[0] != 0 || indices[1] != 2 {
		t.Errorf("expected [0, 2], got %v", indices)
	}
}

func TestSelectedIndices_Sorted(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}

	m := NewSelector(findings)
	// Indices should be sorted regardless of iteration order
	indices := m.SelectedIndices()

	for i := 1; i < len(indices); i++ {
		if indices[i] < indices[i-1] {
			t.Errorf("indices not sorted: %v", indices)
		}
	}
}

func TestUpdate_CursorMoveDown(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
	}
	m := NewSelector(findings)

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = newModel.(SelectorModel)

	if m.cursor != 1 {
		t.Errorf("expected cursor=1, got %d", m.cursor)
	}
}

func TestUpdate_CursorMoveUp(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
	}
	m := NewSelector(findings)
	m.cursor = 1

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = newModel.(SelectorModel)

	if m.cursor != 0 {
		t.Errorf("expected cursor=0, got %d", m.cursor)
	}
}

func TestUpdate_CursorStopsAtTop(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
	}
	m := NewSelector(findings)
	m.cursor = 0

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = newModel.(SelectorModel)

	if m.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", m.cursor)
	}
}

func TestUpdate_CursorStopsAtBottom(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
	}
	m := NewSelector(findings)
	m.cursor = 1

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = newModel.(SelectorModel)

	if m.cursor != 1 {
		t.Errorf("expected cursor to stay at 1, got %d", m.cursor)
	}
}

func TestUpdate_VimKeybindings(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}

	// Test j for down
	m := NewSelector(findings)
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = newModel.(SelectorModel)

	if m.cursor != 1 {
		t.Errorf("j key: expected cursor=1, got %d", m.cursor)
	}

	// Test k for up
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = newModel.(SelectorModel)

	if m.cursor != 0 {
		t.Errorf("k key: expected cursor=0, got %d", m.cursor)
	}
}

func TestUpdate_SpaceTogglesSelection(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
	}
	m := NewSelector(findings)

	// Initially selected, toggle off
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = newModel.(SelectorModel)

	if m.selected[0] {
		t.Error("expected finding to be deselected after space")
	}

	// Toggle back on
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = newModel.(SelectorModel)

	if !m.selected[0] {
		t.Error("expected finding to be selected after second space")
	}
}

func TestUpdate_SelectAll(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}
	m := NewSelector(findings)
	m.selected[0] = false
	m.selected[1] = false

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = newModel.(SelectorModel)

	for i := range findings {
		if !m.selected[i] {
			t.Errorf("expected finding %d to be selected after 'a'", i)
		}
	}
}

func TestUpdate_SelectNone(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}
	m := NewSelector(findings)

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = newModel.(SelectorModel)

	for i := range findings {
		if m.selected[i] {
			t.Errorf("expected finding %d to be deselected after 'n'", i)
		}
	}
}

func TestUpdate_ExpandToggle(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1", Summary: "Summary 1"},
	}
	m := NewSelector(findings)

	// Initially not expanded
	if m.expanded[0] {
		t.Error("expected finding to not be expanded initially")
	}

	// Toggle expand
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = newModel.(SelectorModel)

	if !m.expanded[0] {
		t.Error("expected finding to be expanded after 'e'")
	}

	// Toggle collapse
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = newModel.(SelectorModel)

	if m.expanded[0] {
		t.Error("expected finding to be collapsed after second 'e'")
	}
}

func TestUpdate_EnterConfirms(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
	}
	m := NewSelector(findings)

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SelectorModel)

	if !m.confirmed {
		t.Error("expected confirmed=true after enter")
	}
	if cmd == nil {
		t.Error("expected quit command after enter")
	}
}

func TestUpdate_QuitQuitsImmediately(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
	}
	m := NewSelector(findings)

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = newModel.(SelectorModel)

	if !m.quitted {
		t.Error("expected quitted=true after 'q'")
	}
	if cmd == nil {
		t.Error("expected quit command after 'q'")
	}
}

func TestUpdate_EscQuitsImmediately(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 1"},
	}
	m := NewSelector(findings)

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(SelectorModel)

	if !m.quitted {
		t.Error("expected quitted=true after esc")
	}
	if cmd == nil {
		t.Error("expected quit command after esc")
	}
}

func TestView_EmptyFindings(t *testing.T) {
	m := NewSelector(nil)
	view := m.View()

	if view != "No findings to select.\n" {
		t.Errorf("expected empty findings message, got: %s", view)
	}
}

func TestView_ContainsTitle(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Test Finding Title"},
	}
	m := NewSelector(findings)
	view := m.View()

	if !containsString(view, "Test Finding Title") {
		t.Error("expected view to contain finding title")
	}
}

func TestView_ContainsReviewerCount(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding", ReviewerCount: 3},
	}
	m := NewSelector(findings)
	view := m.View()

	if !containsString(view, "3 reviewers") {
		t.Error("expected view to contain reviewer count")
	}
}

func TestView_SingleReviewerGrammar(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding", ReviewerCount: 1},
	}
	m := NewSelector(findings)
	view := m.View()

	if !containsString(view, "1 reviewer)") {
		t.Error("expected view to show '1 reviewer' (singular)")
	}
}

func TestView_ExpandedShowsSummary(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding", Summary: "This is the detailed summary"},
	}
	m := NewSelector(findings)
	m.expanded[0] = true
	view := m.View()

	if !containsString(view, "detailed summary") {
		t.Error("expected expanded view to contain summary")
	}
}

func TestView_CollapsedHidesSummary(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding", Summary: "This is the detailed summary"},
	}
	m := NewSelector(findings)
	// expanded[0] is false by default
	view := m.View()

	if containsString(view, "detailed summary") {
		t.Error("expected collapsed view to not contain summary")
	}
}

func TestView_ContainsHelpText(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding"},
	}
	m := NewSelector(findings)
	view := m.View()

	if !containsString(view, "navigate") || !containsString(view, "toggle") {
		t.Error("expected view to contain help text")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
