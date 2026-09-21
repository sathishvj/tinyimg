package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"image.jpg", true},
		{"image.JPG", true},
		{"image.jpeg", true},
		{"image.JPEG", true},
		{"image.png", true},
		{"image.PNG", true},
		{"image.webp", true},
		{"image.WEBP", true},
		{"image.txt", false},
		{"image.gif", false},
		{"image", false},
	}

	for _, tc := range tests {
		if got := isImageFile(tc.path); got != tc.expected {
			t.Errorf("isImageFile(%q) = %v; want %v", tc.path, got, tc.expected)
		}
	}
}

func TestCollectInputs(t *testing.T) {
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	file1 := filepath.Join(tmpDir, "img1.png")
	file2 := filepath.Join(tmpDir, "doc.txt")
	file3 := filepath.Join(subDir, "img2.jpg")
	file4 := filepath.Join(subDir, "img3.WEBP")

	for _, f := range []string{file1, file2, file3, file4} {
		if err := os.WriteFile(f, []byte("fake content"), 0o644); err != nil {
			t.Fatalf("failed to write test file %s: %v", f, err)
		}
	}

	t.Run("non-recursive with dir", func(t *testing.T) {
		inputs, err := collectInputs([]string{tmpDir}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(inputs, []string{tmpDir}) {
			t.Errorf("got %v, want [%s]", inputs, tmpDir)
		}
	})

	t.Run("recursive with dir", func(t *testing.T) {
		inputs, err := collectInputs([]string{tmpDir}, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sort.Strings(inputs)
		expected := []string{file1, file3, file4}
		sort.Strings(expected)
		if !reflect.DeepEqual(inputs, expected) {
			t.Errorf("got %v, want %v", inputs, expected)
		}
	})

	t.Run("explicit files", func(t *testing.T) {
		inputs, err := collectInputs([]string{file1, file2}, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(inputs, []string{file1, file2}) {
			t.Errorf("got %v, want [%s %s]", inputs, file1, file2)
		}
	})

	t.Run("unrecognized flag argument", func(t *testing.T) {
		_, err := collectInputs([]string{file1, "-invalid-param"}, true)
		if err == nil {
			t.Fatalf("expected error for unrecognized flag, got nil")
		}
	})
}

func TestCleanupTempFolders(t *testing.T) {
	tmpDir := t.TempDir()
	tempFolder := filepath.Join(tmpDir, ".tinyimg-test123")
	if err := os.Mkdir(tempFolder, 0o755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dummyFile := filepath.Join(tempFolder, "temp.jpg")
	if err := os.WriteFile(dummyFile, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}

	cleanupTempFolders([]string{tmpDir})

	if _, err := os.Stat(tempFolder); !os.IsNotExist(err) {
		t.Errorf("expected tempFolder %s to be deleted, but it still exists", tempFolder)
	}
}
