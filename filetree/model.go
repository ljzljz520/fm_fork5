package filetree

import (
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/mistakenelf/fm/filesystem"
	"github.com/mistakenelf/fm/keys"
)

type treeState int

const (
	IdleState treeState = iota
	CreateFileState
	CreateDirectoryState
	MoveState
	RenameState
)

// TreeID identifies which directory tree a message belongs to.
type TreeID int

const (
	// PrimaryTree is the main, left hand tree.
	PrimaryTree TreeID = iota
	// SecondaryTree is the right hand tree used while moving an item.
	SecondaryTree
)

// OpKind identifies the kind of a submitted filesystem operation.
type OpKind int

const (
	OpCreateFile OpKind = iota
	OpCreateDirectory
	OpRename
	OpMove
)

type DirectoryItem struct {
	Name        string
	Details     string
	Path        string
	Extension   string
	FileSize    string
	IsDirectory bool
	FileInfo    os.FileInfo
}

type Model struct {
	files                 []DirectoryItem
	Cursor                int
	min                   int
	max                   int
	height                int
	width                 int
	Disabled              bool
	showHidden            bool
	showDirectoriesOnly   bool
	showFilesOnly         bool
	showIcons             bool
	keyMap                keys.KeyMap
	startDir              string
	StatusMessage         string
	selectionPath         string
	StatusMessageLifetime time.Duration
	statusMessageTimer    *time.Timer
	selectedItemColor     lipgloss.AdaptiveColor
	unselectedItemColor   lipgloss.AdaptiveColor
	inactiveItemColor     lipgloss.AdaptiveColor
	err                   error
	CurrentDirectory      string
	State                 treeState
	treeID                TreeID
	opSequence            uint64
	pendingOpID           uint64
	pendingKind           OpKind
}

func New(startDir string) Model {
	startingDirectory := filesystem.CurrentDirectory

	if startDir != "" {
		startingDirectory = startDir
	}

	return Model{
		Cursor:                0,
		Disabled:              false,
		keyMap:                keys.DefaultKeyMap(),
		min:                   0,
		max:                   0,
		startDir:              startingDirectory,
		showHidden:            true,
		StatusMessage:         "",
		StatusMessageLifetime: time.Second,
		showFilesOnly:         false,
		showDirectoriesOnly:   false,
		selectedItemColor:     lipgloss.AdaptiveColor{Light: "212", Dark: "212"},
		unselectedItemColor:   lipgloss.AdaptiveColor{Light: "ffffff", Dark: "#000000"},
		inactiveItemColor:     lipgloss.AdaptiveColor{Light: "243", Dark: "243"},
		showIcons:             true,
	}
}
