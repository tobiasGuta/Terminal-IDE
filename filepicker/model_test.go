package filepicker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePickerDoesNotListParentOutsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	model := NewRooted(root, PickFile)
	for _, entry := range model.entries {
		if entry.name == ".." {
			t.Fatalf("did not expect parent entry at root boundary")
		}
	}
}

func TestFilePickerUnrootedAllowsParentTraversal(t *testing.T) {
	root := t.TempDir()
	model := New(root, PickFile)
	foundParent := false
	for _, entry := range model.entries {
		if entry.name == ".." {
			foundParent = true
			break
		}
	}
	if !foundParent {
		t.Fatalf("expected unrooted picker to include parent navigation")
	}
}

func TestIsWithinRootBlocksParentTraversal(t *testing.T) {
	root := filepath.Clean("/tmp/project")
	if isWithinRoot(root, filepath.Join(root, "subdir")) != true {
		t.Fatalf("expected child path to be allowed")
	}
	if isWithinRoot(root, filepath.Clean("/tmp")) {
		t.Fatalf("expected parent path to be blocked")
	}
}
