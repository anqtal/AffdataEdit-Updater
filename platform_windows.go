package main

import (
	"errors"
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
		if errors.Is(err, syscall.Errno(32)) { // ERROR_SHARING_VIOLATION
			return fmt.Errorf("%w: %v", errEditorRunning, err)
		}
		return err
	}
	return syscall.CloseHandle(handle)
}

// Only recognized Unity data and the bundled appearance pack are managed here.
func extraFiles(directory string, remote manifest) ([]string, error) {
	return windowsExtraFiles(directory, remote)
}

func install(directory, stage string, changes []entry, extras []string) error {
	return apply(directory, stage, changes, extras...)
}

func launch(directory string) error {
	cmd := exec.Command(filepath.Join(directory, playerPath))
	cmd.Dir = directory
	return cmd.Start()
}

func finishSuccess() error { return nil }
