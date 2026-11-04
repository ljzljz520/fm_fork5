package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mistakenelf/fm/filetree"
)

// ---- test helpers ---------------------------------------------------------

func newFlowModel(t *testing.T, dir string) model {
	t.Helper()

	return New(Config{StartDir: dir})
}

func send(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()

	updated, _ := m.Update(msg)

	return updated.(model)
}

func runeKey(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func typeKey(typ tea.KeyType) tea.Msg {
	return tea.KeyMsg{Type: typ}
}

func typeString(t *testing.T, m model, s string) model {
	t.Helper()

	for _, r := range s {
		m = send(t, m, runeKey(r))
	}

	return m
}

func requireMsg(t *testing.T, msg tea.Msg) tea.Msg {
	t.Helper()

	if msg == nil {
		t.Fatal("expected a non-nil message")
	}

	return msg
}

func primeListings(t *testing.T, m model, dir string) model {
	t.Helper()

	m = send(t, m, requireMsg(t, m.filetree.GetDirectoryListingCmd(dir)()))
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(dir)()))

	return m
}

// refreshTrees delivers the directory listing responses that the runtime
// would execute after an operation, keeping both trees in sync with disk.
func refreshTrees(t *testing.T, m model) model {
	t.Helper()

	m = send(t, m, requireMsg(t, m.filetree.GetDirectoryListingCmd(m.filetree.CurrentDirectory)()))
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(m.secondaryFiletree.CurrentDirectory)()))

	return m
}

func itemNames(m filetree.Model) []string {
	names := make([]string, 0, len(m.Items()))
	for _, f := range m.Items() {
		names = append(names, f.Name)
	}

	return names
}

func hasItem(m filetree.Model, name string) bool {
	for _, f := range m.Items() {
		if f.Name == name {
			return true
		}
	}

	return false
}

func selectItem(t *testing.T, m model, name string) model {
	t.Helper()

	for i, f := range m.filetree.Items() {
		if f.Name == name {
			m.filetree.Cursor = i
			return m
		}
	}

	t.Fatalf("item %q not found in %v", name, itemNames(m.filetree))

	return m
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertIdle(t *testing.T, m model) {
	t.Helper()

	if m.state != idleState {
		t.Fatalf("expected idle session state, got %v", m.state)
	}

	if m.filetree.State != filetree.IdleState {
		t.Fatalf("expected idle tree state, got %v", m.filetree.State)
	}

	if m.filetree.Disabled {
		t.Fatal("expected the main tree to be enabled")
	}

	if !m.secondaryFiletree.Disabled {
		t.Fatal("expected the secondary tree to be disabled")
	}

	if m.activePane != 0 {
		t.Fatalf("expected main tree focus, got pane %d", m.activePane)
	}

	if m.filetree.IsWaiting() || m.secondaryFiletree.IsWaiting() {
		t.Fatal("expected no operation in flight")
	}
}

// ---- move -----------------------------------------------------------------

func TestMoveSuccessRestoresMainTreeAndSyncsBothTrees(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f.txt"), "payload")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)
	m = selectItem(t, m, "f.txt")

	// Open the move interaction.
	m = send(t, m, runeKey('m'))

	if m.state != showMoveState || m.filetree.State != filetree.MoveState {
		t.Fatalf("expected move interaction, session=%v tree=%v", m.state, m.filetree.State)
	}

	if !m.filetree.Disabled || m.secondaryFiletree.Disabled || m.activePane != 1 {
		t.Fatal("expected focus on an enabled secondary tree")
	}

	if m.moveSourcePath != filepath.Join(dir, "f.txt") || m.moveSourceName != "f.txt" {
		t.Fatalf("source not snapshotted: path=%q name=%q", m.moveSourcePath, m.moveSourceName)
	}

	// Choose the destination. Listings are tagged, so the main tree must keep
	// showing its own directory.
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(filepath.Join(dir, "sub"))()))

	if m.secondaryFiletree.CurrentDirectory != filepath.Join(dir, "sub") {
		t.Fatalf("secondary tree should navigate to sub, got %q", m.secondaryFiletree.CurrentDirectory)
	}

	if m.filetree.CurrentDirectory != dir {
		t.Fatalf("main tree directory was hijacked: %q", m.filetree.CurrentDirectory)
	}

	// Submit: operation is in flight, both panes are locked.
	m = send(t, m, typeKey(tea.KeyEnter))

	pendingID := m.filetree.PendingOperationID()
	if pendingID == 0 || m.pendingMoveID != pendingID {
		t.Fatalf("expected submitted move, pending=%d movePending=%d", pendingID, m.pendingMoveID)
	}

	if !m.filetree.Disabled || !m.secondaryFiletree.Disabled {
		t.Fatal("expected both trees locked while the move is in flight")
	}

	// A second submit must not start another operation.
	m = send(t, m, typeKey(tea.KeyEnter))
	if m.filetree.PendingOperationID() != pendingID || m.pendingMoveID != pendingID {
		t.Fatal("duplicate submit changed the pending operation")
	}

	// Execute the real command against disk.
	destination := filepath.Join(dir, "sub")
	res := m.filetree.MoveDirectoryItemCmd(
		pendingID,
		m.moveSourcePath,
		filepath.Join(destination, m.moveSourceName),
	)().(filetree.OperationResultMsg)

	if res.Err != nil || res.ID != pendingID || res.TreeID != filetree.PrimaryTree || res.Kind != filetree.OpMove {
		t.Fatalf("unexpected move result: %+v", res)
	}

	if _, err := os.Stat(filepath.Join(destination, "f.txt")); err != nil {
		t.Fatalf("expected file at destination on disk: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file gone from source on disk, err=%v", err)
	}

	// Deliver the terminal result: main tree must come back enabled and focused.
	m = send(t, m, res)

	assertIdle(t, m)

	if m.pendingMoveID != 0 || m.moveDestination != "" || m.moveSourcePath != "" {
		t.Fatalf("move bookkeeping not cleared: %+v", m)
	}

	// Both trees refresh to the correct directories and match disk.
	m = refreshTrees(t, m)

	if hasItem(m.filetree, "f.txt") {
		t.Fatal("main tree should no longer list the moved item")
	}

	if !hasItem(m.secondaryFiletree, "f.txt") {
		t.Fatal("secondary tree should list the moved item at the destination")
	}
}

func TestMoveFailurePreservesDestinationAndRetrySucceeds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f.txt"), "payload")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)
	m = selectItem(t, m, "f.txt")

	m = send(t, m, runeKey('m'))
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(filepath.Join(dir, "sub"))()))
	m = send(t, m, typeKey(tea.KeyEnter))

	id := m.filetree.PendingOperationID()
	cmd := m.filetree.MoveDirectoryItemCmd(
		id,
		m.moveSourcePath,
		filepath.Join(m.moveDestination, m.moveSourceName),
	)

	// Controlled disk failure: the source disappears before execution.
	if err := os.Remove(m.moveSourcePath); err != nil {
		t.Fatal(err)
	}

	res := cmd().(filetree.OperationResultMsg)
	if res.Err == nil {
		t.Fatal("expected a failing move result")
	}

	m = send(t, m, res)

	// The interaction stays open with a retryable destination.
	if m.state != showMoveState || m.filetree.State != filetree.MoveState {
		t.Fatalf("expected move interaction to remain open, session=%v tree=%v", m.state, m.filetree.State)
	}

	if !m.filetree.Disabled || m.secondaryFiletree.Disabled {
		t.Fatal("expected main tree locked and secondary destination tree enabled")
	}

	if m.activePane != 1 {
		t.Fatalf("expected focus kept on destination tree, got pane %d", m.activePane)
	}

	if m.secondaryFiletree.CurrentDirectory != filepath.Join(dir, "sub") {
		t.Fatalf("destination directory not preserved: %q", m.secondaryFiletree.CurrentDirectory)
	}

	if m.filetree.IsWaiting() || m.pendingMoveID != 0 {
		t.Fatal("expected failed operation to be no longer pending")
	}

	if _, err := os.Stat(filepath.Join(dir, "sub", "f.txt")); !os.IsNotExist(err) {
		t.Fatal("nothing should have been written to the destination")
	}

	// Retry after restoring the source: a new submission with a fresh id.
	writeFile(t, filepath.Join(dir, "f.txt"), "payload")
	m = send(t, m, typeKey(tea.KeyEnter))

	newID := m.filetree.PendingOperationID()
	if newID == 0 || newID == id {
		t.Fatalf("expected a new operation id on retry, got %d after %d", newID, id)
	}

	if m.moveDestination != filepath.Join(dir, "sub") {
		t.Fatalf("retry lost the destination: %q", m.moveDestination)
	}

	retry := m.filetree.MoveDirectoryItemCmd(
		newID,
		m.moveSourcePath,
		filepath.Join(m.moveDestination, m.moveSourceName),
	)().(filetree.OperationResultMsg)

	if retry.Err != nil {
		t.Fatalf("retry failed: %v", retry.Err)
	}

	m = send(t, m, retry)
	assertIdle(t, m)

	m = refreshTrees(t, m)

	if !hasItem(m.secondaryFiletree, "f.txt") || hasItem(m.filetree, "f.txt") {
		t.Fatal("trees do not match disk after successful retry")
	}
}

func TestLateMoveResultCannotResetLaterInteraction(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f.txt"), "payload")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)
	m = selectItem(t, m, "f.txt")

	// First move submission receives a controlled failure.
	m = send(t, m, runeKey('m'))
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(filepath.Join(dir, "sub"))()))
	m = send(t, m, typeKey(tea.KeyEnter))

	firstID := m.filetree.PendingOperationID()
	m = send(t, m, filetree.OperationResultMsg{
		ID: firstID, Kind: filetree.OpMove, TreeID: filetree.PrimaryTree, Err: errors.New("boom"),
	})

	// Cancel the failed interaction, then open a create-file prompt.
	m = send(t, m, typeKey(tea.KeyEscape))
	assertIdle(t, m)

	m = send(t, m, runeKey('N'))

	if m.filetree.State != filetree.CreateFileState || !m.showTextInput {
		t.Fatal("expected create-file prompt to be open")
	}

	m = typeString(t, m, "late.txt")

	// An out-of-order late SUCCESS from the old move operation arrives.
	m = send(t, m, filetree.OperationResultMsg{
		ID: firstID, Kind: filetree.OpMove, TreeID: filetree.PrimaryTree,
	})

	if m.filetree.State != filetree.CreateFileState || !m.showTextInput {
		t.Fatal("late move result reset the create-file interaction")
	}

	if m.state != idleState {
		t.Fatal("late result must not reopen the move session")
	}

	if m.filetree.Disabled {
		t.Fatal("late result must not disable the main tree")
	}

	if m.textinput.Value() != "late.txt" {
		t.Fatalf("late result must not touch the input, got %q", m.textinput.Value())
	}

	// A result tagged for the other tree is ignored as well.
	m = send(t, m, filetree.OperationResultMsg{
		ID: firstID, Kind: filetree.OpMove, TreeID: filetree.SecondaryTree,
	})

	if m.filetree.State != filetree.CreateFileState || !m.showTextInput {
		t.Fatal("foreign tree result reset the create-file interaction")
	}

	// The newer interaction still reaches its own terminal state normally.
	m = send(t, m, typeKey(tea.KeyEnter))
	secondID := m.filetree.PendingOperationID()
	m = send(t, m, filetree.OperationResultMsg{
		ID: secondID, Kind: filetree.OpCreateFile, TreeID: filetree.PrimaryTree,
	})

	if m.filetree.State != filetree.IdleState || m.showTextInput {
		t.Fatal("current interaction should still reconcile on its own result")
	}

	// The stale move never happened on disk.
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatalf("source file must remain untouched on disk: %v", err)
	}
}

// ---- create ---------------------------------------------------------------

func TestCreateFileFlowSuccessDuplicateGuardAndFailureRetry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)

	// Open the prompt and type a name.
	m = send(t, m, runeKey('N'))
	m = typeString(t, m, "created.txt")

	// Submit: pending, input stays visible but blurred.
	m = send(t, m, typeKey(tea.KeyEnter))

	id := m.filetree.PendingOperationID()
	if id == 0 {
		t.Fatal("expected pending create operation")
	}

	// esc cannot cancel a submitted operation.
	m = send(t, m, typeKey(tea.KeyEscape))

	if !m.filetree.IsWaiting() || m.filetree.State != filetree.CreateFileState {
		t.Fatal("esc must not cancel an in-flight operation")
	}

	// Duplicate submit is swallowed.
	m = send(t, m, typeKey(tea.KeyEnter))
	if m.filetree.PendingOperationID() != id {
		t.Fatal("duplicate submit started another operation")
	}

	// Real disk outcome.
	res := m.filetree.CreateFileCmd(id, "created.txt")().(filetree.OperationResultMsg)
	if res.Err != nil {
		t.Fatalf("create failed: %v", res.Err)
	}

	m = send(t, m, res)

	if m.filetree.State != filetree.IdleState || m.showTextInput || m.textinput.Value() != "" {
		t.Fatal("success should close and reset the prompt")
	}

	m = refreshTrees(t, m)
	if !hasItem(m.filetree, "created.txt") {
		t.Fatal("tree should list the created file")
	}

	if _, err := os.Stat(filepath.Join(dir, "created.txt")); err != nil {
		t.Fatalf("file missing on disk: %v", err)
	}

	// Controlled failure: missing parent directory.
	m = send(t, m, runeKey('N'))
	m = typeString(t, m, "nodir/x.txt")
	m = send(t, m, typeKey(tea.KeyEnter))

	failID := m.filetree.PendingOperationID()
	failed := m.filetree.CreateFileCmd(failID, "nodir/x.txt")().(filetree.OperationResultMsg)
	if failed.Err == nil {
		t.Fatal("expected creation to fail")
	}

	m = send(t, m, failed)

	if m.filetree.State != filetree.CreateFileState || !m.showTextInput || !m.textinput.Focused() {
		t.Fatal("failure should keep an open, focused prompt for retry")
	}

	if m.textinput.Value() != "nodir/x.txt" {
		t.Fatalf("typed input should be preserved, got %q", m.textinput.Value())
	}

	if m.filetree.IsWaiting() {
		t.Fatal("failed operation should no longer be pending")
	}

	if _, err := os.Stat(filepath.Join(dir, "nodir", "x.txt")); !os.IsNotExist(err) {
		t.Fatal("nothing should exist on disk")
	}

	// Fix the input and retry successfully.
	m.textinput.SetValue("")
	m = typeString(t, m, "fixed.txt")
	m = send(t, m, typeKey(tea.KeyEnter))

	retryID := m.filetree.PendingOperationID()
	if retryID == failID {
		t.Fatal("retry should get a fresh operation id")
	}

	ok := m.filetree.CreateFileCmd(retryID, "fixed.txt")().(filetree.OperationResultMsg)
	if ok.Err != nil {
		t.Fatalf("retry create failed: %v", ok.Err)
	}

	m = send(t, m, ok)
	assertIdle(t, m)

	if _, err := os.Stat(filepath.Join(dir, "fixed.txt")); err != nil {
		t.Fatalf("retried file missing on disk: %v", err)
	}
}

func TestCreateDirectoryFlow(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)

	m = send(t, m, runeKey('M'))

	if m.filetree.State != filetree.CreateDirectoryState || !m.showTextInput {
		t.Fatal("expected create-directory prompt")
	}

	m = typeString(t, m, "newdir")
	m = send(t, m, typeKey(tea.KeyEnter))

	id := m.filetree.PendingOperationID()
	res := m.filetree.CreateDirectoryCmd(id, "newdir")().(filetree.OperationResultMsg)
	if res.Err != nil || res.Kind != filetree.OpCreateDirectory {
		t.Fatalf("unexpected create directory result: %+v", res)
	}

	m = send(t, m, res)
	assertIdle(t, m)
	m = refreshTrees(t, m)

	info, err := os.Stat(filepath.Join(dir, "newdir"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected directory on disk, err=%v", err)
	}

	if !hasItem(m.filetree, "newdir") {
		t.Fatal("tree should list the created directory")
	}
}

// ---- rename ---------------------------------------------------------------

func TestRenameSuccessAndFailurePreservesInput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "old.txt"), "x")

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)
	m = selectItem(t, m, "old.txt")

	m = send(t, m, runeKey('R'))
	m = typeString(t, m, "new.txt")
	m = send(t, m, typeKey(tea.KeyEnter))

	id := m.filetree.PendingOperationID()
	res := m.filetree.RenameDirectoryItemCmd(
		id,
		filepath.Join(dir, "old.txt"),
		filepath.Join(m.filetree.CurrentDirectory, "new.txt"),
	)().(filetree.OperationResultMsg)

	if res.Err != nil || res.Kind != filetree.OpRename {
		t.Fatalf("unexpected rename result: %+v", res)
	}

	m = send(t, m, res)
	assertIdle(t, m)
	m = refreshTrees(t, m)

	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatalf("renamed file missing on disk: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("original name should be gone on disk")
	}

	if !hasItem(m.filetree, "new.txt") {
		t.Fatal("tree should list the renamed item")
	}

	// Controlled failure: target parent directory does not exist.
	m = selectItem(t, m, "new.txt")
	m = send(t, m, runeKey('R'))
	m = typeString(t, m, "nodir/new2.txt")
	m = send(t, m, typeKey(tea.KeyEnter))

	failID := m.filetree.PendingOperationID()
	failed := m.filetree.RenameDirectoryItemCmd(
		failID,
		filepath.Join(dir, "new.txt"),
		filepath.Join(m.filetree.CurrentDirectory, "nodir/new2.txt"),
	)().(filetree.OperationResultMsg)

	if failed.Err == nil {
		t.Fatal("expected rename failure")
	}

	m = send(t, m, failed)

	if m.filetree.State != filetree.RenameState || !m.showTextInput || !m.textinput.Focused() {
		t.Fatal("rename failure should keep an editable prompt")
	}

	if m.textinput.Value() != "nodir/new2.txt" {
		t.Fatalf("rename input should be preserved, got %q", m.textinput.Value())
	}

	// Cancelling after a failed attempt leaves disk untouched.
	m = send(t, m, typeKey(tea.KeyEscape))
	assertIdle(t, m)

	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatalf("file must remain under its original name: %v", err)
	}
}

// ---- cancellation ---------------------------------------------------------

func TestCancelBeforeSubmitNeverTouchesDisk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f.txt"), "payload")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newFlowModel(t, dir)
	m = primeListings(t, m, dir)
	m = selectItem(t, m, "f.txt")

	// Cancel an unsubmitted move.
	m = send(t, m, runeKey('m'))
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(filepath.Join(dir, "sub"))()))
	m = send(t, m, typeKey(tea.KeyEscape))

	assertIdle(t, m)

	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatalf("cancel before submit must not touch disk: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "sub", "f.txt")); !os.IsNotExist(err) {
		t.Fatal("nothing should have been moved")
	}

	// Cancel an unsubmitted create prompt.
	m = send(t, m, runeKey('N'))
	m = typeString(t, m, "ghost.txt")
	m = send(t, m, typeKey(tea.KeyEscape))

	assertIdle(t, m)

	if _, err := os.Stat(filepath.Join(dir, "ghost.txt")); !os.IsNotExist(err) {
		t.Fatal("cancelled create must not exist on disk")
	}

	if m.textinput.Value() != "" {
		t.Fatal("cancelled prompt should reset its input")
	}

	// Cancel an unsubmitted rename.
	m = selectItem(t, m, "f.txt")
	m = send(t, m, runeKey('R'))
	m = typeString(t, m, "ghost2.txt")
	m = send(t, m, typeKey(tea.KeyEscape))

	assertIdle(t, m)

	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatalf("cancelled rename must leave disk unchanged: %v", err)
	}

	// Cancelling an in-flight move is ignored; the terminal result wins.
	m = selectItem(t, m, "f.txt")
	m = send(t, m, runeKey('m'))
	m = send(t, m, requireMsg(t, m.secondaryFiletree.GetDirectoryListingCmd(filepath.Join(dir, "sub"))()))
	m = send(t, m, typeKey(tea.KeyEnter))

	inFlightID := m.filetree.PendingOperationID()
	m = send(t, m, typeKey(tea.KeyEscape))

	if m.filetree.PendingOperationID() != inFlightID || m.state != showMoveState {
		t.Fatal("esc must not cancel an already submitted move")
	}

	res := m.filetree.MoveDirectoryItemCmd(
		inFlightID,
		m.moveSourcePath,
		filepath.Join(m.moveDestination, m.moveSourceName),
	)().(filetree.OperationResultMsg)

	if res.Err != nil {
		t.Fatalf("move failed: %v", res.Err)
	}

	m = send(t, m, res)
	assertIdle(t, m)

	if _, err := os.Stat(filepath.Join(dir, "sub", "f.txt")); err != nil {
		t.Fatalf("submitted move must land on disk: %v", err)
	}
}
