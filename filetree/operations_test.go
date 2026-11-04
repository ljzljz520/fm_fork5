package filetree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestTree(t *testing.T, id TreeID, dir string) Model {
	t.Helper()

	m := New(dir)
	m.SetTreeID(id)
	m.StatusMessageLifetime = 0

	return m
}

func deliver(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	updated, _ := m.Update(msg)

	return updated
}

func TestBeginOperationIssuesMonotonicIDs(t *testing.T) {
	m := newTestTree(t, PrimaryTree, t.TempDir())

	first := m.BeginOperation(OpCreateFile)
	second := m.BeginOperation(OpCreateFile)

	if first == 0 || second == 0 {
		t.Fatalf("expected non-zero operation ids, got %d and %d", first, second)
	}

	if second <= first {
		t.Fatalf("expected operation ids to increase, got %d then %d", first, second)
	}

	if m.PendingOperationID() != second || !m.IsWaiting() {
		t.Fatalf("expected pending operation %d, got %d", second, m.PendingOperationID())
	}
}

func TestOperationResultSuccessLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := newTestTree(t, PrimaryTree, dir)
	m.State = CreateFileState

	id := m.BeginOperation(OpCreateFile)

	m = deliver(t, m, OperationResultMsg{ID: id, Kind: OpCreateFile, TreeID: PrimaryTree})

	if m.State != IdleState {
		t.Fatalf("expected idle state after success, got %v", m.State)
	}

	if m.PendingOperationID() != 0 || m.IsWaiting() {
		t.Fatalf("expected no pending operation after success, got %d", m.PendingOperationID())
	}
}

func TestOperationResultFailureKeepsPrompt(t *testing.T) {
	dir := t.TempDir()
	m := newTestTree(t, PrimaryTree, dir)
	m.State = RenameState

	id := m.BeginOperation(OpRename)

	updated, cmd := m.Update(OperationResultMsg{
		ID: id, Kind: OpRename, TreeID: PrimaryTree, Err: errors.New("disk broken"),
	})
	m = updated

	if m.State != RenameState {
		t.Fatalf("expected prompt state to be retained after failure, got %v", m.State)
	}

	if m.PendingOperationID() != 0 {
		t.Fatalf("expected pending operation to be cleared after terminal error, got %d", m.PendingOperationID())
	}

	if cmd == nil {
		t.Fatal("expected an error status command after failure")
	}

	if m.StatusMessage == "" {
		t.Fatal("expected the error to be surfaced as a status message")
	}
}

func TestOperationResultIgnoresForeignTreeAndLateResults(t *testing.T) {
	dir := t.TempDir()
	m := newTestTree(t, PrimaryTree, dir)
	m.State = CreateFileState

	id := m.BeginOperation(OpCreateFile)

	// Result belonging to the secondary tree must be ignored.
	m = deliver(t, m, OperationResultMsg{ID: id, Kind: OpCreateFile, TreeID: SecondaryTree})
	if m.State != CreateFileState || m.PendingOperationID() != id {
		t.Fatal("foreign tree result must not reset the primary interaction")
	}

	// Late result carrying an unknown id must be ignored.
	m = deliver(t, m, OperationResultMsg{ID: id + 99, Kind: OpCreateFile, TreeID: PrimaryTree})
	if m.State != CreateFileState || m.PendingOperationID() != id {
		t.Fatal("late result must not reset the current interaction")
	}

	// A zero id is never a valid terminal result.
	m = deliver(t, m, OperationResultMsg{ID: 0, Kind: OpCreateFile, TreeID: PrimaryTree})
	if m.State != CreateFileState || m.PendingOperationID() != id {
		t.Fatal("zero id result must be ignored")
	}
}

func TestDisabledTreeStillProcessesResultsAndListings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestTree(t, PrimaryTree, dir)
	m.SetDisabled(true)
	m.State = MoveState
	id := m.BeginOperation(OpMove)

	// Keyboard input must remain blocked.
	before := m.Cursor
	m = deliver(t, m, keyRunes('j'))

	if m.Cursor != before {
		t.Fatal("disabled tree must not react to key presses")
	}

	// But the terminal result of the operation must not be dropped.
	m = deliver(t, m, OperationResultMsg{ID: id, Kind: OpMove, TreeID: PrimaryTree})

	if m.State != IdleState {
		t.Fatalf("disabled tree must process completion results, state is %v", m.State)
	}

	if m.PendingOperationID() != 0 {
		t.Fatalf("pending operation should be cleared, got %d", m.PendingOperationID())
	}

	// Directory listings are delivered while disabled too.
	m = deliver(t, m, m.GetDirectoryListingCmd(dir)())

	if m.CurrentDirectory != dir {
		t.Fatalf("expected listing to be applied, current directory %q", m.CurrentDirectory)
	}

	if len(m.files) != 1 || m.files[0].Name != "a.txt" {
		t.Fatalf("expected listing to contain a.txt, got %+v", m.files)
	}

	// A listing tagged for the other tree must not overwrite this tree.
	other := newTestTree(t, SecondaryTree, t.TempDir())
	m = deliver(t, m, other.GetDirectoryListingCmd(other.startDir)())

	if m.CurrentDirectory != dir {
		t.Fatalf("foreign listing changed the tree to %q", m.CurrentDirectory)
	}
}

func TestDirectoryListingErrorDoesNotChangeDirectory(t *testing.T) {
	dir := t.TempDir()
	m := newTestTree(t, PrimaryTree, dir)
	m = deliver(t, m, m.GetDirectoryListingCmd(dir)())

	if m.CurrentDirectory != dir {
		t.Fatalf("expected listing of %q, got %q", dir, m.CurrentDirectory)
	}

	updated, cmd := m.Update(m.GetDirectoryListingCmd(filepath.Join(dir, "does-not-exist"))())
	m = updated

	if m.CurrentDirectory != dir {
		t.Fatalf("failed listing must not change directory, got %q", m.CurrentDirectory)
	}

	if cmd == nil || m.StatusMessage == "" {
		t.Fatal("expected failed listing to surface an error status")
	}
}

func TestCommandsReportDiskOutcomesWithIdentity(t *testing.T) {
	dir := t.TempDir()
	m := newTestTree(t, PrimaryTree, dir)

	// Create file: success.
	filePath := filepath.Join(dir, "new.txt")

	res := m.CreateFileCmd(1, filePath)().(OperationResultMsg)
	if res.Err != nil || res.ID != 1 || res.TreeID != PrimaryTree || res.Kind != OpCreateFile {
		t.Fatalf("unexpected create result: %+v", res)
	}

	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected file to exist on disk: %v", err)
	}

	// Create file: controlled failure (missing parent directory).
	failed := m.CreateFileCmd(2, filepath.Join(dir, "no-such-dir", "x.txt"))().(OperationResultMsg)
	if failed.Err == nil || failed.ID != 2 {
		t.Fatalf("expected failure result with id 2, got %+v", failed)
	}

	// Rename: success and verifiable on disk.
	renamedPath := filepath.Join(dir, "renamed.txt")

	res = m.RenameDirectoryItemCmd(3, filePath, renamedPath)().(OperationResultMsg)
	if res.Err != nil || res.ID != 3 || res.Kind != OpRename {
		t.Fatalf("unexpected rename result: %+v", res)
	}

	if _, err := os.Stat(renamedPath); err != nil {
		t.Fatalf("expected renamed file on disk: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected original path to be gone, got err=%v", err)
	}

	// Move into another directory: success.
	destinationDir := filepath.Join(dir, "dest")
	if err := os.Mkdir(destinationDir, 0o755); err != nil {
		t.Fatal(err)
	}

	movedPath := filepath.Join(destinationDir, "renamed.txt")

	res = m.MoveDirectoryItemCmd(4, renamedPath, movedPath)().(OperationResultMsg)
	if res.Err != nil || res.ID != 4 || res.Kind != OpMove {
		t.Fatalf("unexpected move result: %+v", res)
	}

	if _, err := os.Stat(movedPath); err != nil {
		t.Fatalf("expected moved file on disk: %v", err)
	}

	// Move: controlled failure (source disappeared before execution).
	if err := os.Remove(movedPath); err != nil {
		t.Fatal(err)
	}

	failedMove := m.MoveDirectoryItemCmd(5, movedPath, filepath.Join(destinationDir, "late.txt"))().(OperationResultMsg)
	if failedMove.Err == nil || failedMove.ID != 5 || failedMove.Kind != OpMove {
		t.Fatalf("expected failing move result with id 5, got %+v", failedMove)
	}

	// Secondary tree tags its own results.
	secondary := newTestTree(t, SecondaryTree, t.TempDir())
	secondaryFile := filepath.Join(secondary.startDir, "s.txt")

	res = secondary.CreateFileCmd(6, secondaryFile)().(OperationResultMsg)
	if res.Err != nil || res.TreeID != SecondaryTree || res.ID != 6 {
		t.Fatalf("expected secondary tagged result, got %+v", res)
	}
}

func keyRunes(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
