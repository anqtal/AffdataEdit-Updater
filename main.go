package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type entry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Set for macOS programs and libraries that must keep their execute permission.
	Executable bool `json:"executable,omitempty"`
}
type manifest struct {
	Schema  int     `json:"schema"`
	Version string  `json:"version"`
	Files   []entry `json:"files"`
}
type config struct {
	ManifestURL string `json:"manifestUrl"`
}

const defaultManifestURL = "https://pub-155d0648defb479e9230bc846a4fa5b8.r2.dev/" + platform + "/latest.json"

var client = &http.Client{
	Timeout: 30 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" {
			return errors.New("invalid download redirect")
		}
		return nil
	},
}

func main() {
	directory := flag.String("install-dir", "", "AffdataEdit installation directory")
	source := flag.String("manifest-url", "", "HTTPS URL of "+platform+"/latest.json")
	flag.Parse()
	if err := run(*directory, *source); err != nil {
		fmt.Fprintln(os.Stderr, "更新失败：", err)
		fmt.Println("按 Enter 关闭。")
		_, _ = fmt.Scanln()
		os.Exit(1)
	}
}

func run(directory, source string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if directory == "" {
		directory = defaultInstallDir(self)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	if source == "" {
		data, err := os.ReadFile(filepath.Join(directory, "updater-config.json"))
		if os.IsNotExist(err) {
			source = defaultManifestURL
		} else if err != nil {
			return fmt.Errorf("read updater-config.json: %w", err)
		} else {
			var settings config
			if err = json.Unmarshal(data, &settings); err != nil {
				return err
			}
			source = settings.ManifestURL
		}
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("manifest URL must be HTTPS without a query or fragment")
	}
	if err = os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	// Deny concurrent updaters; the OS releases the lock even after a crash.
	unlock, err := lockInstallation(filepath.Join(directory, ".affdata-update.lock"))
	if err != nil {
		return fmt.Errorf("cannot lock installation (another updater or no write permission): %w", err)
	}
	defer unlock()
	fmt.Println("正在检查更新…")
	body, err := get(source)
	if err != nil {
		return err
	}
	var remote manifest
	err = json.NewDecoder(io.LimitReader(body, 16<<20)).Decode(&remote)
	body.Close()
	if err != nil {
		return err
	}
	if remote.Schema != 1 || remote.Version == "" || len(remote.Files) == 0 {
		return errors.New("unsupported or empty manifest")
	}
	seen := map[string]bool{}
	for _, file := range remote.Files {
		if err = validate(directory, file); err != nil {
			return err
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return fmt.Errorf("duplicate path: %s", file.Path)
		}
		seen[key] = true
	}
	if !seen[strings.ToLower(playerPath)] {
		return fmt.Errorf("manifest does not contain %s", playerPath)
	}
	stage, err := os.MkdirTemp(directory, ".affdata-update-")
	if err != nil {
		return err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			os.RemoveAll(stage)
		}
	}()
	var changes []entry
	for _, file := range remote.Files {
		// AffdataEdit keeps the updater current; it never replaces itself.
		if strings.EqualFold(file.Path, updaterName) {
			continue
		}
		target := filepath.Join(directory, filepath.FromSlash(file.Path))
		equal, err := matches(target, file)
		if err != nil {
			return err
		}
		if equal {
			// An identical file that lost its execute permission only needs it restored.
			if err = setExecutable(target, file); err != nil {
				return err
			}
			continue
		}
		fmt.Println("下载：", file.Path)
		objectURL := parsed.ResolveReference(&url.URL{Path: "objects/" + file.SHA256}).String()
		destination := filepath.Join(stage, "new", filepath.FromSlash(file.Path))
		if err = download(objectURL, destination, file); err != nil {
			return err
		}
		changes = append(changes, file)
	}
	extras, err := extraFiles(directory, remote)
	if err != nil {
		return err
	}
	if len(changes) == 0 && len(extras) == 0 {
		fmt.Println("所有文件已是最新版本。")
	} else {
		fmt.Printf("%d 个文件需要更新。请先保存并关闭 AffdataEdit，再按 Enter 继续。\n", len(changes)+len(extras))
		_, _ = fmt.Scanln()
		if err = ensureClosed(filepath.Join(directory, filepath.FromSlash(playerPath))); err != nil {
			return err
		}
		if err = install(directory, stage, changes, extras); err != nil {
			keepStage = true
			return fmt.Errorf("%w; backup retained at %s", err, stage)
		}
		fmt.Println("更新完成：", remote.Version)
	}
	if err = installSelf(self, directory); err != nil {
		fmt.Fprintln(os.Stderr, "无法把更新器复制到安装目录：", err)
	}
	return launch(directory)
}

// A first install copies the downloaded updater beside AffdataEdit, where AffdataEdit
// keeps it up to date; the downloaded copy is no longer needed.
func installSelf(self, directory string) error {
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	if filepath.Dir(self) == directory {
		return nil
	}
	target := filepath.Join(directory, updaterName)
	temp := target + ".new"
	if err := copyFile(self, temp); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Chmod(temp, 0755); err != nil {
		os.Remove(temp)
		return err
	}
	return os.Rename(temp, target)
}

func validate(root string, file entry) error {
	if file.Size < 0 || file.Size > 1<<40 || len(file.SHA256) != 64 {
		return errors.New("invalid file metadata")
	}
	if _, err := hex.DecodeString(file.SHA256); err != nil {
		return err
	}
	if file.Path == "" || strings.ContainsAny(file.Path, "\\:<>\"|?*\x00") {
		return fmt.Errorf("unsafe path: %q", file.Path)
	}
	current := root
	for _, part := range strings.Split(file.Path, "/") {
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		reserved := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9')
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.HasPrefix(strings.ToLower(part), ".affdata-update") || reserved {
			return fmt.Errorf("unsafe path: %q", file.Path)
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing linked path: %s", current)
		}
	}
	return nil
}

func matches(path string, file entry) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() != file.Size {
		return false, nil
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), file.SHA256), nil
}

func get(address string) (io.ReadCloser, error) {
	response, err := client.Get(address)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %d downloading %s", response.StatusCode, address)
	}
	return response.Body, nil
}

func download(address, path string, file entry) error {
	body, err := get(address)
	if err != nil {
		return err
	}
	defer body.Close()
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var mode os.FileMode = 0644
	if file.Executable {
		mode = 0755
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(body, file.Size+1))
	closeErr := out.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	ok, err := matches(path, file)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("hash/size mismatch: %s", file.Path)
	}
	return nil
}

func setExecutable(path string, file entry) error {
	if !file.Executable {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode()&0111 == 0111 {
		return err
	}
	return os.Chmod(path, info.Mode()|0111)
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}

func apply(root, stage string, files []entry) (result error) {
	type change struct {
		target, backup   string
		installed, saved bool
	}
	var completed []*change
	defer func() {
		if result == nil {
			return
		}
		for i := len(completed) - 1; i >= 0; i-- {
			c := completed[i]
			if c.installed {
				result = errors.Join(result, os.Remove(c.target))
			}
			if c.saved {
				result = errors.Join(result, os.Rename(c.backup, c.target))
			}
		}
	}()
	for _, file := range files {
		if err := validate(root, file); err != nil {
			return err
		}
		relative := filepath.FromSlash(file.Path)
		c := &change{target: filepath.Join(root, relative), backup: filepath.Join(stage, "old", relative)}
		completed = append(completed, c)
		if err := os.MkdirAll(filepath.Dir(c.backup), 0755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(c.target), 0755); err != nil {
			return err
		}
		if _, err := os.Stat(c.target); err == nil {
			if err = os.Rename(c.target, c.backup); err != nil {
				return err
			}
			c.saved = true
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(filepath.Join(stage, "new", relative), c.target); err != nil {
			return err
		}
		c.installed = true
	}
	return nil
}
