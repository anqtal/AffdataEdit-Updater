package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// These are Unity-generated files, not arbitrary files in the installation.
var unityDataFile = regexp.MustCompile(`(?i)^(globalgamemanagers(\.assets)?|resources\.assets|sharedassets[0-9]+\.assets|level[0-9]+)(\.ress|\.resource)?$|^data\.unity3d$`)
var releaseVersion = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func managedCleanupPath(path string) bool {
	parts := strings.Split(strings.ToLower(path), "/")
	if len(parts) < 2 || parts[0] != "affdataedit_data" {
		return false
	}
	return len(parts) == 2 && unityDataFile.MatchString(parts[1]) ||
		len(parts) > 3 && parts[1] == "streamingassets" && parts[2] == "appearance"
}

// Missing history is not evidence of ownership. Continue the update without
// cleanup instead of guessing from file names or recursively mirroring folders.
func windowsExtraFiles(directory string, remote manifest, source string) ([]entry, error) {
	previous, err := previousManifest(directory, source)
	if err != nil {
		fmt.Println("无法核对旧版本清单，保留旧文件并继续更新：", err)
		return nil, nil
	}
	return windowsRetiredFiles(directory, previous, remote)
}

func previousManifest(directory, source string) (manifest, error) {
	var previous manifest
	if err := validatePath(directory, "build-version.txt"); err != nil {
		return previous, err
	}
	data, err := os.ReadFile(filepath.Join(directory, "build-version.txt"))
	if err != nil {
		return previous, err
	}
	version := strings.TrimSpace(string(data))
	if !releaseVersion.MatchString(version) {
		return previous, errors.New("invalid installed version")
	}
	base, err := url.Parse(source)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.RawQuery != "" || base.Fragment != "" {
		return previous, errors.New("invalid manifest URL")
	}
	address := base.ResolveReference(&url.URL{Path: "releases/" + version + ".json"}).String()
	// History is optional; do not stall a valid update for the download timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return previous, err
	}
	response, err := client.Do(request)
	if err != nil {
		return previous, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return previous, fmt.Errorf("previous manifest: HTTP %d", response.StatusCode)
	}
	const limit = 16 << 20
	data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return previous, err
	}
	if len(data) > limit {
		return previous, errors.New("previous manifest too large")
	}
	if err := json.Unmarshal(data, &previous); err != nil {
		return previous, err
	}
	if previous.Schema != 1 || !strings.EqualFold(previous.Version, version) || len(previous.Files) == 0 {
		return previous, errors.New("invalid previous manifest")
	}
	seen := make(map[string]bool, len(previous.Files))
	for _, file := range previous.Files {
		if err := validate(directory, file); err != nil {
			return previous, err
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return previous, fmt.Errorf("duplicate previous path: %s", file.Path)
		}
		seen[key] = true
	}
	if !seen["affdataedit.exe"] {
		return previous, errors.New("previous manifest does not contain AffdataEdit.exe")
	}
	return previous, nil
}

func windowsRetiredFiles(directory string, previous, remote manifest) ([]entry, error) {
	expected := make(map[string]bool, len(remote.Files))
	for _, file := range remote.Files {
		expected[strings.ToLower(file.Path)] = true
	}
	var extras []entry
	for _, file := range previous.Files {
		if !managedCleanupPath(file.Path) || expected[strings.ToLower(file.Path)] {
			continue
		}
		if err := validate(directory, file); err != nil {
			return nil, err
		}
		equal, err := matches(filepath.Join(directory, filepath.FromSlash(file.Path)), file)
		if err != nil {
			return nil, err
		}
		if equal {
			extras = append(extras, file)
		}
	}
	return extras, nil
}
