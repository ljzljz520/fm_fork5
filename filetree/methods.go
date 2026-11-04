package filetree

import (
	"github.com/charmbracelet/lipgloss"
)

// SetDisabled sets if the bubble is currently active.
func (m *Model) SetDisabled(disabled bool) {
	m.Disabled = disabled
}

// SetTreeID assigns the tree identity used to tag commands and to decide
// which result messages belong to this tree.
func (m *Model) SetTreeID(id TreeID) {
	m.treeID = id
}

// BeginOperation marks an operation as submitted and waiting for a terminal
// result. It returns the unique id that the result message must carry.
func (m *Model) BeginOperation(kind OpKind) uint64 {
	m.opSequence++
	m.pendingOpID = m.opSequence
	m.pendingKind = kind

	return m.opSequence
}

// PendingOperationID returns the id of the operation currently in flight, or
// zero when no operation is waiting for a result.
func (m Model) PendingOperationID() uint64 {
	return m.pendingOpID
}

// IsWaiting reports whether a submitted operation is still waiting for its
// terminal result.
func (m Model) IsWaiting() bool {
	return m.pendingOpID != 0
}

// GetSelectedItem returns the currently selected file/dir.
func (m Model) GetSelectedItem() DirectoryItem {
	if len(m.files) > 0 {
		return m.files[m.Cursor]
	}

	return DirectoryItem{}
}

// GetTotalItems returns total number of tree items.
func (m Model) GetTotalItems() int {
	return len(m.files)
}

// Items returns the directory items currently listed.
func (m Model) Items() []DirectoryItem {
	return m.files
}

// SetSize Sets the size of the filetree.
func (m *Model) SetSize(width, height int) {
	m.height = height
	m.width = width
	m.max = m.height - 1
}

// SetTheme sets the theme of the tree.
func (m *Model) SetTheme(selectedItemColor, unselectedItemColor lipgloss.AdaptiveColor) {
	m.selectedItemColor = selectedItemColor
	m.unselectedItemColor = unselectedItemColor
}

// SetSelectionPath sets the selection path to be written.
func (m *Model) SetSelectionPath(path string) {
	m.selectionPath = path
}

// SetShowIcons sets whether icons will show or not.
func (m *Model) SetShowIcons(show bool) {
	m.showIcons = show
}
