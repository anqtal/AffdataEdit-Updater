package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeCleanupFile(t *testing.T, root, relative string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(relative), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsCleanupKeepsCurrentAndUserFiles(t *testing.T) {
	root := t.TempDir()
	retired := []string{
		"AffdataEdit_Data/globalgamemanagers",
		"AffdataEdit_Data/resources.assets.resS",
		"AffdataEdit_Data/SharedAssets0.assets",
		"AffdataEdit_Data/level0.resource",
		"AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/old.png",
		"AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/Map.json",
	}
	kept := []string{
		"AffdataEdit.exe", "AffdataEdit-Updater.exe", "settings.json", "account.json",
		"Background/custom.png", "Skin/Arcade-editor-skin.json", "Charts/local.aff", "custom.txt",
		"AffdataEdit_Data/notes.aff", "AffdataEdit_Data/Managed/custom.dll",
		"AffdataEdit_Data/StreamingAssets/other/custom.png",
		"AffdataEdit_Data/data.unity3d",
		"AffdataEdit_Data/StreamingAssets/Appearance/Catalogue.json",
		"AffdataEdit_Data/StreamingAssets/Appearance/Images/diamond.png",
	}
	for _, path := range append(slices.Clone(retired), kept...) {
		writeCleanupFile(t, root, path)
	}
	remote := manifest{Files: []entry{
		{Path: "AffdataEdit.exe"},
		{Path: "AffdataEdit_Data/DATA.UNITY3D"},
		{Path: "AffdataEdit_Data/StreamingAssets/Appearance/Catalogue.json"},
		{Path: "AffdataEdit_Data/StreamingAssets/Appearance/Images/diamond.png"},
	}}
	extras, err := windowsExtraFiles(root, remote)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(retired)
	slices.Sort(extras)
	if !slices.Equal(retired, extras) {
		t.Fatalf("retired files = %v; want %v", extras, retired)
	}
	// A fully current installation must still remove obsolete data without downloads.
	if err := apply(root, t.TempDir(), nil, extras...); err != nil {
		t.Fatal(err)
	}
	for _, path := range retired {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("obsolete file remains: %s (%v)", path, err)
		}
	}
	for _, path := range kept {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || string(data) != path {
			t.Fatalf("current/user file changed: %s (%v)", path, err)
		}
	}
}

func TestWindowsCleanupRollsBackWithFailedInstall(t *testing.T) {
	root, stage := t.TempDir(), t.TempDir()
	obsolete := "AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/old.png"
	writeCleanupFile(t, root, obsolete)
	writeCleanupFile(t, root, "AffdataEdit.exe")
	writeCleanupFile(t, stage, "new/AffdataEdit.exe")
	files := []entry{
		{Path: "AffdataEdit.exe", SHA256: strings.Repeat("0", 64)},
		{Path: "missing.dll", SHA256: strings.Repeat("0", 64)},
	}
	if err := apply(root, stage, files, obsolete); err == nil {
		t.Fatal("expected the missing staged file to fail installation")
	}
	for _, path := range []string{obsolete, "AffdataEdit.exe"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || string(data) != path {
			t.Fatalf("old file not restored: %s (%v)", path, err)
		}
	}
}

func TestWindowsCleanupRejectsLinkedPaths(t *testing.T) {
	for _, relative := range []string{
		"AffdataEdit_Data",
		"AffdataEdit_Data/StreamingAssets/Appearance/Diamonds",
	} {
		t.Run(relative, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			writeCleanupFile(t, outside, "resources.assets")
			link := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symbolic links unavailable: %v", err)
			}
			if _, err := windowsExtraFiles(root, manifest{}); err == nil {
				t.Fatal("linked program directory was accepted")
			}
			data, err := os.ReadFile(filepath.Join(outside, "resources.assets"))
			if err != nil || string(data) != "resources.assets" {
				t.Fatalf("linked user file changed: %v", err)
			}
		})
	}
}
