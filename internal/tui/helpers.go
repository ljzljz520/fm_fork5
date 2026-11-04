package tui

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/lipgloss"
	"github.com/mistakenelf/fm/filetree"
	"github.com/mistakenelf/fm/polish"
)

func contains(s []string, str string) bool {
	return slices.Contains(s, str)
}

// operationInFlight reports whether either tree has submitted an operation
// that is still waiting for its terminal result.
func (m *model) operationInFlight() bool {
	return m.filetree.IsWaiting() || m.secondaryFiletree.IsWaiting()
}

// cancelInteraction closes any open create/rename/move interaction without a
// disk operation having happened and restores the main tree as the active,
// enabled pane. It must only be called when no operation is in flight.
func (m *model) cancelInteraction() {
	m.state = idleState
	m.showTextInput = false
	m.disableAllViewports()
	m.resetViewports()
	m.filetree.SetDisabled(false)
	m.textinput.Blur()
	m.textinput.Reset()
	m.filetree.State = filetree.IdleState
	m.secondaryFiletree.SetDisabled(true)
	m.activePane = 0
	m.pendingMoveID = 0
	m.moveSourcePath = ""
	m.moveSourceName = ""
	m.moveDestination = ""
}

func (m *model) disableAllViewports() {
	m.code.SetViewportDisabled(true)
	m.pdf.SetViewportDisabled(true)
	m.markdown.SetViewportDisabled(true)
	m.help.SetViewportDisabled(true)
	m.image.SetViewportDisabled(true)
	m.csv.SetViewportDisabled(true)
}

func (m *model) resetViewports() {
	m.code.GotoTop()
	m.pdf.GotoTop()
	m.markdown.GotoTop()
	m.help.GotoTop()
	m.image.GotoTop()
	m.csv.GotoTop()
}

func (m *model) updateStatusBar() {
	if m.filetree.GetSelectedItem().Name != "" {
		statusMessage :=
			m.filetree.CurrentDirectory +
				lipgloss.NewStyle().
					Padding(0, 1).
					Foreground(polish.Colors.Yellow500).
					Render(m.filetree.GetSelectedItem().Details) +
				lipgloss.NewStyle().
					Padding(0, 1).
					Foreground(polish.Colors.Pink).
					Render(m.filetree.GetSelectedItem().
						FileInfo.
						ModTime().Format("2006-01-02"))

		if m.filetree.StatusMessage != "" {
			statusMessage = m.filetree.StatusMessage
		}

		if m.code.StatusMessage != "" {
			statusMessage = m.code.StatusMessage
		}

		if m.markdown.StatusMessage != "" {
			statusMessage = m.markdown.StatusMessage
		}

		if m.pdf.StatusMessage != "" {
			statusMessage = m.pdf.StatusMessage
		}

		if m.image.StatusMessage != "" {
			statusMessage = m.image.StatusMessage
		}

		if m.statusMessage != "" {
			statusMessage = m.statusMessage
		}

		if m.showTextInput {
			// Keep a terminal operation error visible next to the prompt so
			// a failed submit is not silent while the input is retained.
			inputView := m.textinput.View()
			if m.filetree.StatusMessage != "" || m.statusMessage != "" {
				statusMessage = statusMessage + " " + inputView
			} else {
				statusMessage = inputView
			}
		}

		m.statusbar.SetContent(
			m.filetree.GetSelectedItem().Name,
			statusMessage,
			fmt.Sprintf("%d/%d", m.filetree.Cursor+1, m.filetree.GetTotalItems()),
			fmt.Sprintf("%s", m.filetree.GetSelectedItem().FileSize),
		)
	} else {
		statusMessage := "Directory is empty"

		if m.showTextInput {
			inputView := m.textinput.View()
			if m.filetree.StatusMessage != "" || m.statusMessage != "" {
				statusMessage = m.filetree.StatusMessage + m.statusMessage + " " + inputView
			} else {
				statusMessage = inputView
			}
		}

		m.statusbar.SetContent(
			"N/A",
			statusMessage,
			fmt.Sprintf("%d/%d", 0, 0),
			"FM",
		)
	}
}
