package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateKeepsExtraFilesAndRollsBackOnFailure(t *testing.T) {
	root := t.TempDir()
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	file := entry{Path: "player.txt", SHA256: strings.Repeat("0", 64), Size: 3}
	write(filepath.Join(root, "player.txt"), "old")
	write(filepath.Join(root, "My charts", "local.aff"), "user chart")
	stage := t.TempDir()
	write(filepath.Join(stage, "new", "player.txt"), "new")
	if err := apply(root, stage, []entry{file}); err != nil {
		t.Fatal(err)
	}
	if read(filepath.Join(root, "player.txt")) != "new" || read(filepath.Join(root, "My charts", "local.aff")) != "user chart" {
		t.Fatal("update damaged local files")
	}

	nextStage := t.TempDir()
	write(filepath.Join(nextStage, "new", "player.txt"), "bad")
	missing := entry{Path: "missing.txt", SHA256: file.SHA256, Size: 3}
	if err := apply(root, nextStage, []entry{file, missing}); err == nil {
		t.Fatal("expected missing staged file to fail")
	}
	if read(filepath.Join(root, "player.txt")) != "new" {
		t.Fatal("previous file was not restored")
	}
	if read(filepath.Join(root, "My charts", "local.aff")) != "user chart" {
		t.Fatal("extra file was modified")
	}
}
