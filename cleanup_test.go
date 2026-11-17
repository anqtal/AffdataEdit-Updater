package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func writeCleanupFile(t *testing.T, root, relative string, contents ...string) entry {
	t.Helper()
	data := relative
	if len(contents) > 0 {
		data = contents[0]
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(data))
	return entry{Path: relative, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data))}
}

func requireCleanupFile(t *testing.T, root, relative, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil || string(data) != want {
		t.Fatalf("file changed: %s = %q (%v); want %q", relative, data, err, want)
	}
}

func TestWindowsCleanupKeepsCurrentModifiedAndUserFiles(t *testing.T) {
	root := t.TempDir()
	previous := manifest{}
	retired := []string{
		"AffdataEdit_Data/globalgamemanagers", "AffdataEdit_Data/resources.assets.resS",
		"AffdataEdit_Data/SharedAssets0.assets", "AffdataEdit_Data/level0.resource",
		"AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/old.png",
		"AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/Map.json",
	}
	kept := []string{
		"AffdataEdit.exe", "AffdataEdit-Updater.exe", "settings.json", "settings.json.bak", "account.json",
		"Background/custom.png", "Skin/Arcade-editor-skin.json", "Charts/local.aff", "My charts/music.aff",
		"Cache/SlowAudio/cached.flac", "custom.txt", "AffdataEdit_Data/notes.aff",
		"AffdataEdit_Data/Managed/custom.dll", "AffdataEdit_Data/StreamingAssets/other/custom.png",
		"AffdataEdit_Data/data.unity3d", "AffdataEdit_Data/StreamingAssets/Appearance/Catalogue.json",
		"AffdataEdit_Data/StreamingAssets/Appearance/Images/diamond.png",
	}
	for _, path := range append(slices.Clone(retired), kept...) {
		previous.Files = append(previous.Files, writeCleanupFile(t, root, path))
	}
	// Even known-looking files with no proof in the previous manifest belong to the user.
	unknown := []string{"AffdataEdit_Data/StreamingAssets/Appearance/Backgrounds/custom.png", "AffdataEdit_Data/level99"}
	for _, path := range unknown {
		writeCleanupFile(t, root, path)
	}
	modified := []string{"AffdataEdit_Data/sharedassets1.assets", "AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/edited.png"}
	for _, path := range modified {
		previous.Files = append(previous.Files, writeCleanupFile(t, root, path, "original"))
		writeCleanupFile(t, root, path, "modified") // Same length: size alone is insufficient.
	}
	remote := manifest{Files: []entry{
		{Path: "AffdataEdit.exe"}, {Path: "AffdataEdit_Data/DATA.UNITY3D"},
		{Path: "AffdataEdit_Data/StreamingAssets/Appearance/Catalogue.json"},
		{Path: "AffdataEdit_Data/StreamingAssets/Appearance/Images/diamond.png"},
	}}
	extras, err := windowsRetiredFiles(root, previous, remote)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range extras {
		paths = append(paths, file.Path)
	}
	slices.Sort(paths)
	slices.Sort(retired)
	if !slices.Equal(paths, retired) {
		t.Fatalf("retired files = %v; want %v", paths, retired)
	}
	stage := t.TempDir()
	if err := apply(root, stage, nil, extras...); err != nil {
		t.Fatal(err)
	}
	for _, path := range retired {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("obsolete file remains: %s (%v)", path, err)
		}
		requireCleanupFile(t, filepath.Join(stage, "old"), path, path)
	}
	for _, path := range append(kept, unknown...) {
		requireCleanupFile(t, root, path, path)
	}
	for _, path := range modified {
		requireCleanupFile(t, root, path, "modified")
	}
}

func TestWindowsCleanupRollsBackWithFailedInstall(t *testing.T) {
	root, stage := t.TempDir(), t.TempDir()
	var extras []entry
	retired := []string{"AffdataEdit_Data/resources.assets.resS", "AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/old.png"}
	for _, path := range retired {
		extras = append(extras, writeCleanupFile(t, root, path))
	}
	old := writeCleanupFile(t, root, "AffdataEdit.exe")
	for _, path := range []string{"Background/custom.png", "Charts/custom.aff", "settings.json"} {
		writeCleanupFile(t, root, path)
	}
	replacement := writeCleanupFile(t, stage, "new/AffdataEdit.exe", "new executable")
	replacement.Path = old.Path
	missing := entry{Path: "missing.dll", SHA256: strings.Repeat("0", 64)}
	if err := apply(root, stage, []entry{replacement, missing}, extras...); err == nil {
		t.Fatal("expected missing staged file to fail installation")
	}
	for _, path := range append(retired, "AffdataEdit.exe", "Background/custom.png", "Charts/custom.aff", "settings.json") {
		requireCleanupFile(t, root, path, path)
	}
	if _, err := os.Stat(filepath.Join(root, "missing.dll")); !os.IsNotExist(err) {
		t.Fatal("failed install left a new file")
	}
}

func TestWindowsCleanupRechecksHashBeforeRemoval(t *testing.T) {
	root := t.TempDir()
	path := "AffdataEdit_Data/StreamingAssets/Appearance/Diamonds/changed.png"
	original := writeCleanupFile(t, root, path, "original")
	extras, err := windowsRetiredFiles(root, manifest{Files: []entry{original}}, manifest{})
	if err != nil || len(extras) != 1 {
		t.Fatalf("plan failed: %v (%v)", extras, err)
	}
	writeCleanupFile(t, root, path, "modified")
	if err := apply(root, t.TempDir(), nil, extras...); err != nil {
		t.Fatal(err)
	}
	requireCleanupFile(t, root, path, "modified")
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
	if err := apply(root, t.TempDir(), nil, extras...); err != nil {
		t.Fatal("already removed file should be harmless:", err)
	}
}

func TestWindowsCleanupRejectsUnmanagedRemoval(t *testing.T) {
	for _, path := range []string{"Background/custom.png", "Charts/custom.aff", "settings.json", "Cache/SlowAudio/song.flac", "AffdataEdit-Updater.exe"} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			file := writeCleanupFile(t, root, path)
			if err := apply(root, t.TempDir(), nil, file); err == nil {
				t.Fatal("unmanaged deletion accepted")
			}
			requireCleanupFile(t, root, path, path)
		})
	}
}

func TestWindowsCleanupRejectsLinkedPaths(t *testing.T) {
	for _, relative := range []string{"AffdataEdit_Data", "AffdataEdit_Data/StreamingAssets/Appearance/Diamonds"} {
		t.Run(relative, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			name := "resources.assets"
			if strings.HasSuffix(relative, "Diamonds") {
				name = "old.png"
			}
			file := writeCleanupFile(t, outside, name)
			file.Path = relative + "/" + name
			link := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symbolic links unavailable: %v", err)
			}
			if _, err := windowsRetiredFiles(root, manifest{Files: []entry{file}}, manifest{}); err == nil {
				t.Fatal("linked program directory accepted")
			}
			if err := apply(root, t.TempDir(), nil, file); err == nil {
				t.Fatal("linked deletion accepted")
			}
			requireCleanupFile(t, outside, name, name)
		})
	}
}

func cleanupHistoryServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	old := client
	client = server.Client()
	t.Cleanup(func() { client = old; server.Close() })
	return server.URL + "/channel/windows/latest.json"
}

func TestWindowsCleanupLoadsArchivedManifestAndOnlyRemovesVerifiedFiles(t *testing.T) {
	root := t.TempDir()
	version := strings.Repeat("a", 40)
	writeCleanupFile(t, root, "build-version.txt", version+"\n")
	player := writeCleanupFile(t, root, "AffdataEdit.exe")
	retired := writeCleanupFile(t, root, "AffdataEdit_Data/resources.assets.resS")
	previous := manifest{Schema: 1, Version: version, Files: []entry{player, retired}}
	source := cleanupHistoryServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channel/windows/releases/"+version+".json" {
			t.Errorf("wrong archive URL: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(previous); err != nil {
			t.Error(err)
		}
	})
	remote := manifest{Files: []entry{player}}
	extras, err := windowsExtraFiles(root, remote, source)
	if err != nil || len(extras) != 1 || extras[0] != retired {
		t.Fatalf("cleanup plan = %v (%v)", extras, err)
	}
	if err := apply(root, t.TempDir(), nil, extras...); err != nil {
		t.Fatal(err)
	}
	requireCleanupFile(t, root, player.Path, player.Path)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(retired.Path))); !os.IsNotExist(err) {
		t.Fatal("retired data remains")
	}
}

func TestWindowsCleanupSkipsUnverifiableHistory(t *testing.T) {
	for _, kind := range []string{"missing-version", "invalid-version", "404", "malformed", "wrong-version", "wrong-schema", "invalid-hash", "unsafe-path", "duplicate", "missing-player", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			version := strings.Repeat("a", 40)
			if kind != "missing-version" {
				writeCleanupFile(t, root, "build-version.txt", version)
			}
			if kind == "invalid-version" {
				writeCleanupFile(t, root, "build-version.txt", "../old")
			}
			player := writeCleanupFile(t, root, "AffdataEdit.exe")
			retired := writeCleanupFile(t, root, "AffdataEdit_Data/resources.assets.resS")
			previous := manifest{Schema: 1, Version: version, Files: []entry{player, retired}}
			switch kind {
			case "wrong-version":
				previous.Version = strings.Repeat("b", 40)
			case "wrong-schema":
				previous.Schema = 2
			case "invalid-hash":
				previous.Files[1].SHA256 = "bad"
			case "unsafe-path":
				previous.Files[1].Path = "AffdataEdit_Data/../settings.json"
			case "duplicate":
				previous.Files = append(previous.Files, previous.Files[1])
			case "missing-player":
				previous.Files = previous.Files[1:]
			}
			var requests atomic.Int32
			source := cleanupHistoryServer(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if kind == "404" {
					http.NotFound(w, r)
					return
				}
				if kind == "malformed" {
					fmt.Fprint(w, "not json")
					return
				}
				if kind == "oversized" {
					fmt.Fprint(w, strings.Repeat(" ", (16<<20)+1))
					return
				}
				if err := json.NewEncoder(w).Encode(previous); err != nil {
					t.Error(err)
				}
			})
			extras, err := windowsExtraFiles(root, manifest{Files: []entry{player}}, source)
			if err != nil || len(extras) != 0 {
				t.Fatalf("unverified cleanup: %v (%v)", extras, err)
			}
			if (kind == "missing-version" || kind == "invalid-version") && requests.Load() != 0 {
				t.Fatal("invalid version issued HTTP request")
			}
			// An unavailable cleanup archive must not prevent ordinary replacements.
			stage := t.TempDir()
			next := writeCleanupFile(t, stage, "new/AffdataEdit.exe", "new executable")
			next.Path = player.Path
			if err := apply(root, stage, []entry{next}, extras...); err != nil {
				t.Fatal(err)
			}
			requireCleanupFile(t, root, player.Path, "new executable")
			requireCleanupFile(t, root, retired.Path, retired.Path)
		})
	}
}

func TestFailedDownloadDoesNotCleanOldFiles(t *testing.T) {
	root := t.TempDir()
	player := writeCleanupFile(t, root, "AffdataEdit.exe", "old executable")
	retired := writeCleanupFile(t, root, "AffdataEdit_Data/resources.assets.resS")
	writeCleanupFile(t, root, "settings.json", "user settings")
	version := strings.Repeat("a", 40)
	writeCleanupFile(t, root, "build-version.txt", version)
	current := manifest{Schema: 1, Version: strings.Repeat("b", 40), Files: []entry{{Path: playerPath, Size: 3, SHA256: strings.Repeat("0", 64)}}}
	var archiveRequested atomic.Bool
	source := cleanupHistoryServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/") {
			archiveRequested.Store(true)
		}
		if strings.HasSuffix(r.URL.Path, "/latest.json") {
			if err := json.NewEncoder(w).Encode(current); err != nil {
				t.Error(err)
			}
			return
		}
		fmt.Fprint(w, "bad")
	})
	if err := run(root, source); err == nil {
		t.Fatal("expected invalid downloaded hash to fail")
	}
	if archiveRequested.Load() {
		t.Fatal("cleanup planning ran before download verification")
	}
	requireCleanupFile(t, root, player.Path, "old executable")
	requireCleanupFile(t, root, retired.Path, retired.Path)
	requireCleanupFile(t, root, "settings.json", "user settings")
	if dirs, err := filepath.Glob(filepath.Join(root, ".affdata-update-*")); err != nil || len(dirs) != 0 {
		t.Fatalf("failed download left staging folders: %v (%v)", dirs, err)
	}
}
