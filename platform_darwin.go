package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	platform    = "macos"
	bundle      = "AffdataEdit.app"
	playerPath  = bundle + "/Contents/MacOS/AffdataEdit"
	updaterName = "AffdataEdit-Updater"
)

// An updater beside AffdataEdit.app updates it; a downloaded updater installs
// /Applications/AffdataEdit and copies itself there.
func defaultInstallDir(self string) string {
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	directory := filepath.Dir(self)
	if _, err := os.Stat(filepath.Join(directory, bundle)); err == nil {
		return directory
	}
	return "/Applications/AffdataEdit"
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

// Nothing of the user's belongs inside the app, so files a new build no longer has are removed.
// Paths are relative to the app.
func extraFiles(directory string, remote manifest) ([]string, error) {
	expected := map[string]bool{}
	for _, file := range remote.Files {
		expected[strings.ToLower(file.Path)] = true
	}
	app := filepath.Join(directory, bundle)
	var extras []string
	err := filepath.WalkDir(app, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == app {
				return filepath.SkipAll
			}
			return err
		}
		if item.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(app, path)
		if err != nil {
			return err
		}
		if !expected[strings.ToLower(bundle+"/"+filepath.ToSlash(relative))] {
			extras = append(extras, relative)
		}
		return nil
	})
	return extras, err
}

// The app is replaced as a whole: changes go into an APFS clone of it, which is then swapped
// in by renaming, so an interrupted update leaves either the old or the new app, never a mix.
func install(directory, stage string, changes []entry, extras []string) (result error) {
	var inside, outside []entry
	for _, file := range changes {
		if strings.HasPrefix(file.Path, bundle+"/") {
			inside = append(inside, file)
		} else {
			outside = append(outside, file)
		}
	}
	app := filepath.Join(directory, bundle)
	old := filepath.Join(stage, "old", bundle)
	swapped, hadApp := false, false
	if len(inside) > 0 || len(extras) > 0 {
		work := filepath.Join(stage, "work", bundle)
		if err := os.MkdirAll(filepath.Dir(work), 0755); err != nil {
			return err
		}
		if _, err := os.Stat(app); err == nil {
			hadApp = true
			// ditto clones files on APFS and keeps permissions, links and attributes.
			if output, err := exec.Command("ditto", app, work).CombinedOutput(); err != nil {
				return fmt.Errorf("copy %s: %v: %s", bundle, err, strings.TrimSpace(string(output)))
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		for _, file := range inside {
			target := filepath.Join(work, filepath.FromSlash(strings.TrimPrefix(file.Path, bundle+"/")))
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Rename(filepath.Join(stage, "new", filepath.FromSlash(file.Path)), target); err != nil {
				return err
			}
		}
		for _, extra := range extras {
			if err := os.Remove(filepath.Join(work, extra)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if hadApp {
			if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
				return err
			}
			if err := os.Rename(app, old); err != nil {
				return err
			}
		}
		if err := os.Rename(work, app); err != nil {
			if hadApp {
				err = errors.Join(err, os.Rename(old, app))
			}
			return err
		}
		swapped = true
	}
	defer func() {
		if result == nil || !swapped {
			return
		}
		// Put the previous app back when the files beside it could not be updated.
		failed := filepath.Join(stage, "failed", bundle)
		result = errors.Join(result, os.MkdirAll(filepath.Dir(failed), 0755), os.Rename(app, failed))
		if hadApp {
			result = errors.Join(result, os.Rename(old, app))
		}
	}()
	return apply(directory, stage, outside)
}

func launch(directory string) error {
	return exec.Command("open", filepath.Join(directory, bundle)).Run()
}
