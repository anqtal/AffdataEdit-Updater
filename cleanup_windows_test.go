package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Junctions require no symbolic-link privilege on Windows. Exercise the actual
// reparse-point boundary even on hosts where the portable symlink tests skip.
func TestWindowsCleanupRejectsJunctions(t *testing.T) {
	for _, relative := range []string{"AffdataEdit_Data", "AffdataEdit_Data/StreamingAssets/Appearance/Diamonds"} {
		t.Run(relative, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			name := "resources.assets"
			if filepath.Base(relative) == "Diamonds" {
				name = "old.png"
			}
			file := writeCleanupFile(t, outside, name)
			file.Path = relative + "/" + name
			link := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
				t.Fatalf("create junction: %v: %s", err, output)
			}
			if _, err := windowsRetiredFiles(root, manifest{Files: []entry{file}}, manifest{}); err == nil {
				t.Fatal("junction accepted for cleanup planning")
			}
			if err := apply(root, t.TempDir(), nil, file); err == nil {
				t.Fatal("junction accepted for deletion")
			}
			requireCleanupFile(t, outside, name, name)
		})
	}
}
