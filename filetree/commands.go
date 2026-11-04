package filetree

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mistakenelf/fm/filesystem"
)

type errorMsg string
type copyToClipboardMsg string
type statusMessageTimeoutMsg struct{}
type editorFinishedMsg struct{ err error }

// OperationResultMsg is the terminal result of a filesystem operation that
// was explicitly submitted. ID identifies the submission so that late or
// out-of-order results cannot reset a newer interaction, and TreeID is the
// tree that owns the operation.
type OperationResultMsg struct {
	ID     uint64
	Kind   OpKind
	TreeID TreeID
	Err    error
}

type getDirectoryListingMsg struct {
	treeID           TreeID
	files            []DirectoryItem
	workingDirectory string
	err              error
}

// NewStatusMessageCmd sets a new status message, which will show for a limited
// amount of time. Note that this also returns a command.
func (m *Model) NewStatusMessageCmd(s string) tea.Cmd {
	m.StatusMessage = s

	if m.statusMessageTimer != nil {
		m.statusMessageTimer.Stop()
	}

	m.statusMessageTimer = time.NewTimer(m.StatusMessageLifetime)

	return func() tea.Msg {
		<-m.statusMessageTimer.C
		return statusMessageTimeoutMsg{}
	}
}

// CreateDirectoryCmd creates a directory based on the name provided. The id
// must come from BeginOperation and uniquely identifies the submission.
func (m Model) CreateDirectoryCmd(id uint64, name string) tea.Cmd {
	return func() tea.Msg {
		err := filesystem.CreateDirectory(name)

		return OperationResultMsg{
			ID:     id,
			Kind:   OpCreateDirectory,
			TreeID: m.treeID,
			Err:    err,
		}
	}
}

// CreateFileCmd creates a file based on the name provided. The id must come
// from BeginOperation and uniquely identifies the submission.
func (m Model) CreateFileCmd(id uint64, name string) tea.Cmd {
	return func() tea.Msg {
		err := filesystem.CreateFile(name)

		return OperationResultMsg{
			ID:     id,
			Kind:   OpCreateFile,
			TreeID: m.treeID,
			Err:    err,
		}
	}
}

// RenameDirectoryItemCmd renames a file or folder given a name. The id must
// come from BeginOperation and uniquely identifies the submission.
func (m Model) RenameDirectoryItemCmd(id uint64, originalName, newName string) tea.Cmd {
	return func() tea.Msg {
		err := filesystem.RenameDirectoryItem(originalName, newName)

		return OperationResultMsg{
			ID:     id,
			Kind:   OpRename,
			TreeID: m.treeID,
			Err:    err,
		}
	}
}

// MoveDirectoryItemCmd moves an item from one place to another. The id must
// come from BeginOperation and uniquely identifies the submission.
func (m Model) MoveDirectoryItemCmd(id uint64, source, destination string) tea.Cmd {
	return func() tea.Msg {
		err := filesystem.MoveDirectoryItem(source, destination)

		return OperationResultMsg{
			ID:     id,
			Kind:   OpMove,
			TreeID: m.treeID,
			Err:    err,
		}
	}
}

// listingError returns a directory listing message tagged with this tree
// that carries the given error.
func (m Model) listingError(err error) tea.Msg {
	return getDirectoryListingMsg{treeID: m.treeID, err: err}
}

// GetDirectoryListingCmd updates the directory listing based on the name of the directory provided.
func (m Model) GetDirectoryListingCmd(directoryName string) tea.Cmd {
	return func() tea.Msg {
		var err error
		var directoryItems []DirectoryItem
		var files []fs.DirEntry
		var directoryPath string

		if directoryName == filesystem.HomeDirectory {
			directoryName, err = filesystem.GetHomeDirectory()
			if err != nil {
				return m.listingError(err)
			}
		}

		if !filepath.IsAbs(directoryName) {
			directoryPath, err = filepath.Abs(directoryName)
			fmt.Println(directoryPath)
			if err != nil {
				return m.listingError(err)
			}
		} else {
			directoryPath = directoryName
		}

		directoryInfo, err := os.Stat(directoryPath)
		if err != nil {
			return m.listingError(err)
		}

		if !directoryInfo.IsDir() {
			return nil
		}

		if !m.showDirectoriesOnly && !m.showFilesOnly {
			files, err = filesystem.GetDirectoryListing(directoryName, m.showHidden)
			if err != nil {
				return m.listingError(err)
			}
		} else {
			listingType := filesystem.DirectoriesListingType

			if m.showFilesOnly {
				listingType = filesystem.FilesListingType
			}

			files, err = filesystem.GetDirectoryListingByType(directoryName, listingType, m.showHidden)
			if err != nil {
				return m.listingError(err)
			}
		}

		for _, file := range files {
			fileInfo, err := file.Info()
			if err != nil {
				continue
			}

			filePath := filepath.Join(directoryPath, file.Name())
			isDirectory := fileInfo.IsDir()

			isSymlink := fileInfo.Mode()&os.ModeSymlink != 0

			if isSymlink {
				symlinkPath, err := filepath.EvalSymlinks(filepath.Join(directoryPath, file.Name()))
				if err != nil {
					// This could be due to symlinks pointing to invalid path
					continue
				}
				filePath = symlinkPath
				symlinkInfo, err := os.Stat(symlinkPath)
				if err != nil {
					return m.listingError(err)
				}

				isDirectory = symlinkInfo.IsDir()
			}

			fileSize := filesystem.ConvertBytesToSizeString(fileInfo.Size())

			directoryItems = append(directoryItems, DirectoryItem{
				Name:        file.Name(),
				Details:     fileInfo.Mode().String(),
				Path:        filePath,
				Extension:   filepath.Ext(fileInfo.Name()),
				IsDirectory: isDirectory,
				FileInfo:    fileInfo,
				FileSize:    fileSize,
			})
		}

		sort.Slice(directoryItems, func(i, j int) bool {
			return directoryItems[j].FileInfo.ModTime().Before(directoryItems[i].FileInfo.ModTime())
		})

		return getDirectoryListingMsg{
			treeID:           m.treeID,
			files:            directoryItems,
			workingDirectory: directoryPath,
		}
	}
}

// deleteDirectoryItemCmd deletes a directory based on the name provided.
func deleteDirectoryItemCmd(name string, isDirectory bool) tea.Cmd {
	return func() tea.Msg {
		if isDirectory {
			if err := filesystem.DeleteDirectory(name); err != nil {
				return errorMsg(err.Error())
			}
		} else {
			if err := filesystem.DeleteFile(name); err != nil {
				return errorMsg(err.Error())
			}
		}

		return nil
	}
}

// zipDirectoryCmd zips a directory based on the name provided.
func zipDirectoryCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if err := filesystem.Zip(name); err != nil {
			return errorMsg(err.Error())
		}

		return nil
	}
}

// unzipDirectoryCmd unzips a directory based on the name provided.
func unzipDirectoryCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if err := filesystem.Unzip(name); err != nil {
			return errorMsg(err.Error())
		}

		return nil
	}
}

// copyDirectoryItemCmd copies a directory based on the name provided.
func copyDirectoryItemCmd(name string, isDirectory bool) tea.Cmd {
	return func() tea.Msg {
		if isDirectory {
			if err := filesystem.CopyDirectory(name); err != nil {
				return errorMsg(err.Error())
			}
		} else {
			if err := filesystem.CopyFile(name); err != nil {
				return errorMsg(err.Error())
			}
		}

		return nil
	}
}

// copyToClipboardCmd copies the provided string to the clipboard.
func copyToClipboardCmd(path string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(path)
		if err != nil {
			return errorMsg(err.Error())
		}

		return copyToClipboardMsg(
			fmt.Sprintf("%s %s %s", "Successfully copied", path, "to clipboard"),
		)
	}
}

// writeSelectionPathCmd writes content to the file specified.
func writeSelectionPathCmd(selectionPath, filePath string) tea.Cmd {
	return func() tea.Msg {
		if err := filesystem.WriteToFile(selectionPath, filePath); err != nil {
			return errorMsg(err.Error())
		}

		return nil
	}
}

func openEditorCmd(file string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	c := exec.Command(editor, file)

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err}
	})
}
