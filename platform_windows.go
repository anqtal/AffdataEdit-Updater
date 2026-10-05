package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	platform    = "windows"
	playerPath  = "AffdataEdit.exe"
	updaterName = "AffdataEdit-Updater.exe"
)

// The updater lives beside AffdataEdit.exe; a first install uses the updater's directory.
func defaultInstallDir(self string) string {
	return filepath.Dir(self)
}

func lockInstallation(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	lock, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { syscall.CloseHandle(lock) }, nil
}

// A running AffdataEdit.exe cannot be opened exclusively.
func ensureClosed(player string) error {
	if _, err := os.Stat(player); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(player)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("please close AffdataEdit before updating: %w", err)
	}
	return syscall.CloseHandle(handle)
}

// Files the user added to the installation directory are kept.
func extraFiles(directory string, remote manifest) ([]string, error) {
	return nil, nil
}

func install(directory, stage string, changes []entry, extras []string) error {
	return apply(directory, stage, changes)
}

func launch(directory string) error {
	cmd := exec.Command(filepath.Join(directory, playerPath))
	cmd.Dir = directory
	return cmd.Start()
}
