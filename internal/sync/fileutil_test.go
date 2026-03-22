package sync

import (
	"testing"
)

// === AutoRename Tests ===

func TestAutoRenameNoCollision(t *testing.T) {
	result := AutoRename("notes.pdf", []string{"other.doc", "readme.txt"})
	if result != "notes.pdf" {
		t.Errorf("expected notes.pdf, got %s", result)
	}
}

func TestAutoRenameWithCollision(t *testing.T) {
	existing := []string{"notes.pdf"}
	result := AutoRename("notes.pdf", existing)
	if result != "notes(1).pdf" {
		t.Errorf("expected notes(1).pdf, got %s", result)
	}
}

func TestAutoRenameWithMultipleCollisions(t *testing.T) {
	existing := []string{"notes.pdf", "notes(1).pdf", "notes(2).pdf"}
	result := AutoRename("notes.pdf", existing)
	if result != "notes(3).pdf" {
		t.Errorf("expected notes(3).pdf, got %s", result)
	}
}

func TestAutoRenameFolder(t *testing.T) {
	// Folders have no extension
	existing := []string{"Folder"}
	result := AutoRename("Folder", existing)
	if result != "Folder(1)" {
		t.Errorf("expected Folder(1), got %s", result)
	}
}

func TestAutoRenameFolderMultiple(t *testing.T) {
	existing := []string{"Folder", "Folder(1)", "Folder(2)"}
	result := AutoRename("Folder", existing)
	if result != "Folder(3)" {
		t.Errorf("expected Folder(3), got %s", result)
	}
}

func TestAutoRenameEmptyList(t *testing.T) {
	result := AutoRename("document.txt", []string{})
	if result != "document.txt" {
		t.Errorf("expected document.txt, got %s", result)
	}
}

func TestAutoRenameDoubleExtension(t *testing.T) {
	existing := []string{"archive.tar.gz"}
	result := AutoRename("archive.tar.gz", existing)
	// filepath.Ext returns ".gz", so name="archive.tar"
	if result != "archive.tar(1).gz" {
		t.Errorf("expected archive.tar(1).gz, got %s", result)
	}
}

// === IsCircularMove Tests ===

func TestIsCircularMoveDetectsDirectCycle(t *testing.T) {
	ancestors := []int64{100, 50, 25, 10}
	if !IsCircularMove(50, ancestors) {
		t.Error("expected circular move to be detected")
	}
}

func TestIsCircularMoveSafePath(t *testing.T) {
	ancestors := []int64{100, 50, 25, 10}
	if IsCircularMove(200, ancestors) {
		t.Error("expected move to be safe")
	}
}

func TestIsCircularMoveWithRoot(t *testing.T) {
	ancestors := []int64{100, 50, 25}
	if IsCircularMove(0, ancestors) {
		t.Error("moving to root should be safe")
	}
}

func TestIsCircularMoveEmptyAncestors(t *testing.T) {
	if IsCircularMove(100, []int64{}) {
		t.Error("empty ancestors should not detect cycle")
	}
}

// === SplitNameExt Tests ===

func TestSplitNameExtSimple(t *testing.T) {
	name, ext := SplitNameExt("notes.pdf")
	if name != "notes" || ext != ".pdf" {
		t.Errorf("expected (notes, .pdf), got (%s, %s)", name, ext)
	}
}

func TestSplitNameExtNoExtension(t *testing.T) {
	name, ext := SplitNameExt("README")
	if name != "README" || ext != "" {
		t.Errorf("expected (README, ), got (%s, %s)", name, ext)
	}
}

func TestSplitNameExtMultipleDots(t *testing.T) {
	name, ext := SplitNameExt("archive.tar.gz")
	// filepath.Ext only returns the last extension
	if name != "archive.tar" || ext != ".gz" {
		t.Errorf("expected (archive.tar, .gz), got (%s, %s)", name, ext)
	}
}

func TestSplitNameExtHiddenFile(t *testing.T) {
	name, ext := SplitNameExt(".gitignore")
	// filepath.Ext(".gitignore") returns ".gitignore" (treats it as extension)
	if name != "" || ext != ".gitignore" {
		t.Errorf("expected (, .gitignore), got (%s, %s)", name, ext)
	}
}

func TestSplitNameExtEmpty(t *testing.T) {
	name, ext := SplitNameExt("")
	if name != "" || ext != "" {
		t.Errorf("expected (, ), got (%s, %s)", name, ext)
	}
}

func TestSplitNameExtOnlyExtension(t *testing.T) {
	name, ext := SplitNameExt(".txt")
	// filepath.Ext(".txt") returns ".txt" (treats it as extension)
	if name != "" || ext != ".txt" {
		t.Errorf("expected (, .txt), got (%s, %s)", name, ext)
	}
}
