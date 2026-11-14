package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// These are Unity-generated files, not arbitrary files in the installation.
var unityDataFile = regexp.MustCompile(`(?i)^(globalgamemanagers(\.assets)?|resources\.assets|sharedassets[0-9]+\.assets|level[0-9]+)(\.ress|\.resource)?$|^data\.unity3d$`)

func windowsExtraFiles(directory string, remote manifest) ([]string, error) {
	expected := make(map[string]bool, len(remote.Files))
	for _, file := range remote.Files {
		expected[strings.ToLower(file.Path)] = true
	}
	var extras []string
	collect := func(relative string, item fs.DirEntry) error {
		if !item.Type().IsRegular() {
			return fmt.Errorf("not a regular file: %s", filepath.Join(directory, relative))
		}
		if !expected[strings.ToLower(filepath.ToSlash(relative))] {
			extras = append(extras, filepath.ToSlash(relative))
		}
		return nil
	}
	const dataPath = "AffdataEdit_Data"
	if err := validatePath(directory, dataPath); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(filepath.Join(directory, dataPath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if unityDataFile.MatchString(item.Name()) {
			if err := collect(filepath.Join(dataPath, item.Name()), item); err != nil {
				return nil, err
			}
		}
	}
	// The built-in appearance pack is program-owned. User Background, Skin, Charts,
	// settings and any other StreamingAssets folders are deliberately not scanned.
	appearancePath := dataPath + "/StreamingAssets/Appearance"
	if err := validatePath(directory, appearancePath); err != nil {
		return nil, err
	}
	appearance := filepath.Join(directory, filepath.FromSlash(appearancePath))
	err = filepath.WalkDir(appearance, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == appearance {
				return filepath.SkipAll
			}
			return err
		}
		if item.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		return collect(relative, item)
	})
	return extras, err
}
