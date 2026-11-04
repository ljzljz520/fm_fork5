package tui

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mistakenelf/fm/filetree"
	"github.com/mistakenelf/fm/statusbar"
)

// Update handles all UI interactions.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case statusMessageTimeoutMsg:
		m.statusMessage = ""

		return m, nil
	case filetree.OperationResultMsg:
		// Reconcile only the interaction that is currently waiting on this
		// exact result. Results owned by the other tree, or late results from
		// an older submission, must never reset a newer interaction.
		if msg.TreeID == filetree.PrimaryTree && msg.ID == m.filetree.PendingOperationID() {
			switch msg.Kind {
			case filetree.OpCreateFile, filetree.OpCreateDirectory, filetree.OpRename:
				if msg.Err != nil {
					// Nothing landed: keep the prompt and the typed value so
					// the user can edit and retry, or cancel.
					m.showTextInput = true
					m.textinput.Focus()
				} else {
					m.showTextInput = false
					m.activePane = 0
					m.textinput.Blur()
					m.textinput.Reset()
				}
			case filetree.OpMove:
				destination := m.moveDestination
				m.pendingMoveID = 0

				if msg.Err != nil {
					// Keep the move interaction open. The secondary tree
					// preserves the selected destination for a retry.
					m.secondaryFiletree.SetDisabled(false)
					m.activePane = 1
				} else {
					// Terminal success: restore focus to the main tree. The
					// owning tree refreshes its own directory below; refresh
					// the destination directory in the secondary tree too.
					m.state = idleState
					m.filetree.SetDisabled(false)
					m.secondaryFiletree.SetDisabled(true)
					m.activePane = 0
					m.showTextInput = false
					m.textinput.Blur()
					m.textinput.Reset()
					m.moveSourcePath = ""
					m.moveSourceName = ""
					m.moveDestination = ""
					m.disableAllViewports()
					m.resetViewports()

					if destination != "" {
						cmds = append(cmds, m.secondaryFiletree.GetDirectoryListingCmd(destination))
					}
				}
			}
		}
	case tea.WindowSizeMsg:
		halfSize := msg.Width / 2
		height := msg.Height - statusbar.Height

		cmds = append(cmds, m.image.SetSizeCmd(halfSize, height))
		cmds = append(cmds, m.markdown.SetSizeCmd(halfSize, height))
		cmds = append(cmds, m.csv.SetSizeCmd(halfSize, height))

		m.filetree.SetSize(halfSize, height-3)
		m.secondaryFiletree.SetSize(halfSize, height-3)
		m.code.SetSize(halfSize, height)
		m.pdf.SetSize(halfSize, height)
		m.statusbar.SetSize(msg.Width)
		m.help.SetSize(halfSize, height)

		return m, tea.Batch(cmds...)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keyMap.ForceQuit):
			return m, tea.Quit
		case key.Matches(msg, m.keyMap.Quit):
			if m.filetree.State == filetree.IdleState {
				return m, tea.Quit
			}
		case key.Matches(msg, m.keyMap.OpenFile):
			if !m.showTextInput && m.activePane == 0 {
				cmds = append(cmds, m.openFileCmd())
			}
		case key.Matches(msg, m.keyMap.ResetState):
			// A submitted operation may already be on disk, so it cannot be
			// cancelled. Its terminal result will reconcile the interaction.
			if m.operationInFlight() {
				return m, nil
			}

			// Cancelling a move before submission restores the main tree's
			// original directory listing.
			if m.state == showMoveState {
				cmds = append(cmds, m.filetree.GetDirectoryListingCmd(m.directoryBeforeMove))
			}

			m.cancelInteraction()
		case key.Matches(msg, m.keyMap.MoveDirectoryItem):
			if m.activePane == 0 && m.filetree.State == filetree.IdleState && !m.operationInFlight() {
				selected := m.filetree.GetSelectedItem()
				if selected.Name == "" {
					break
				}

				// Snapshot the source when the interaction starts so the
				// later submission is explicit and independent of cursor
				// movement while choosing the destination.
				m.moveSourcePath = selected.Path
				m.moveSourceName = selected.Name
				m.moveDestination = ""
				m.pendingMoveID = 0
				m.directoryBeforeMove = m.filetree.CurrentDirectory
				m.activePane = 1
				m.state = showMoveState
				m.filetree.State = filetree.MoveState
				m.filetree.SetDisabled(true)
				m.secondaryFiletree.SetDisabled(false)
				cmds = append(cmds, m.secondaryFiletree.GetDirectoryListingCmd(m.filetree.CurrentDirectory))
			}
		case key.Matches(msg, m.keyMap.ShowTextInput):
			if m.activePane == 0 && m.filetree.State == filetree.IdleState {
				m.showTextInput = true
				m.textinput.Focus()
				m.disableAllViewports()

				m.textinput, cmd = m.textinput.Update(msg)
				cmds = append(cmds, cmd)

				m.textinput.Reset()
			}
		case key.Matches(msg, m.keyMap.Submit):
			// A submitted operation is waiting for its terminal result:
			// swallow the key to prevent duplicate submissions.
			if m.operationInFlight() {
				return m, nil
			}

			switch m.filetree.State {
			case filetree.CreateFileState:
				name := m.textinput.Value()
				if name == "" {
					cmds = append(cmds, m.newErrorStatusMessageCmd("A file name is required"))

					break
				}

				id := m.filetree.BeginOperation(filetree.OpCreateFile)
				m.textinput.Blur()
				cmds = append(cmds, m.filetree.CreateFileCmd(id, name))
			case filetree.CreateDirectoryState:
				name := m.textinput.Value()
				if name == "" {
					cmds = append(cmds, m.newErrorStatusMessageCmd("A directory name is required"))

					break
				}

				id := m.filetree.BeginOperation(filetree.OpCreateDirectory)
				m.textinput.Blur()
				cmds = append(cmds, m.filetree.CreateDirectoryCmd(id, name))
			case filetree.MoveState:
				if m.state != showMoveState {
					return m, nil
				}

				destinationDir := m.secondaryFiletree.CurrentDirectory
				id := m.filetree.BeginOperation(filetree.OpMove)
				m.pendingMoveID = id
				m.moveDestination = destinationDir
				// Lock both trees until the terminal result arrives.
				m.secondaryFiletree.SetDisabled(true)
				cmds = append(
					cmds,
					m.filetree.MoveDirectoryItemCmd(
						id,
						m.moveSourcePath,
						filepath.Join(destinationDir, m.moveSourceName),
					),
				)
			case filetree.RenameState:
				newName := m.textinput.Value()
				if newName == "" {
					cmds = append(cmds, m.newErrorStatusMessageCmd("A new name is required"))

					break
				}

				selected := m.filetree.GetSelectedItem()
				id := m.filetree.BeginOperation(filetree.OpRename)
				m.textinput.Blur()
				cmds = append(cmds,
					m.filetree.RenameDirectoryItemCmd(
						id,
						selected.Path,
						filepath.Join(m.filetree.CurrentDirectory, newName),
					),
				)
			default:
				return m, nil
			}
		case key.Matches(msg, m.keyMap.TogglePane):
			if !m.showTextInput && !m.operationInFlight() {
				m.activePane = (m.activePane + 1) % 2

				if m.activePane == 0 {
					m.filetree.SetDisabled(false)
					m.secondaryFiletree.SetDisabled(true)
					m.disableAllViewports()
				} else {
					m.filetree.SetDisabled(true)

					switch m.state {
					case idleState:
						m.disableAllViewports()
						m.help.SetViewportDisabled(false)
					case showCodeState:
						m.disableAllViewports()
						m.code.SetViewportDisabled(false)
					case showImageState:
						m.disableAllViewports()
						m.image.SetViewportDisabled(false)
					case showPdfState:
						m.disableAllViewports()
						m.pdf.SetViewportDisabled(false)
					case showMarkdownState:
						m.disableAllViewports()
						m.markdown.SetViewportDisabled(false)
					case showMoveState:
						m.secondaryFiletree.SetDisabled(false)
						m.disableAllViewports()
					}
				}
			}
		case key.Matches(msg, m.keyMap.GotoTop):
			if m.activePane != 0 {
				m.resetViewports()
			}
		case key.Matches(msg, m.keyMap.GotoBottom):
			if m.activePane != 0 {
				m.code.GotoBottom()
				m.pdf.GotoBottom()
				m.markdown.GotoBottom()
				m.help.GotoBottom()
				m.image.GotoBottom()
			}
		}
	}

	if m.filetree.State == filetree.CreateDirectoryState ||
		m.filetree.State == filetree.CreateFileState ||
		m.filetree.State == filetree.RenameState {
		m.textinput, cmd = m.textinput.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.filetree, cmd = m.filetree.Update(msg)
	cmds = append(cmds, cmd)

	m.secondaryFiletree, cmd = m.secondaryFiletree.Update(msg)
	cmds = append(cmds, cmd)

	m.code, cmd = m.code.Update(msg)
	cmds = append(cmds, cmd)

	m.markdown, cmd = m.markdown.Update(msg)
	cmds = append(cmds, cmd)

	m.image, cmd = m.image.Update(msg)
	cmds = append(cmds, cmd)

	m.pdf, cmd = m.pdf.Update(msg)
	cmds = append(cmds, cmd)

	m.help, cmd = m.help.Update(msg)
	cmds = append(cmds, cmd)

	m.csv, cmd = m.csv.Update(msg)
	cmds = append(cmds, cmd)

	m.updateStatusBar()

	return m, tea.Batch(cmds...)
}
