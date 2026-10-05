package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	platform   = "macos"
	playerPath = "AffdataEdit.app/Contents/MacOS/AffdataEdit"
	// Every installed file, including the updater and its settings, stays inside the app.
	appDir = "AffdataEdit.app/"
	// Files are replaced by renaming, which is safe for the running updater, so no copy is needed.
	runFromCopy = false
)

// The updater inside AffdataEdit.app/Contents/MacOS updates that app; a downloaded
// updater installs /Applications/AffdataEdit.app.
func defaultInstallDir(self string) string {
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	if filepath.Base(app) == "AffdataEdit.app" {
		return filepath.Dir(app)
	}
	return "/Applications"
}

func lockInstallation(path string) (func(), error) {
	lock, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, err
	}
	return func() { lock.Close() }, nil
}

func ensureClosed(player string) error {
	// pgrep exits with 1 when no process has this name.
	err := exec.Command("pgrep", "-x", filepath.Base(player)).Run()
	if err == nil {
		return errors.New("please close AffdataEdit before updating")
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil
	}
	return err
}

func launch(directory string) error {
	return exec.Command("open", filepath.Join(directory, "AffdataEdit.app")).Run()
}
