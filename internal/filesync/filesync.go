// Package filesync copies a local project into a session directory and
// returns the files the agent changed.
package filesync

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// MaxFiles is how many project files one snapshot may contain.
	MaxFiles = 4000
	// MaxFileBytes is the largest single file.
	MaxFileBytes = 1 << 20
	// MaxTotalBytes is the largest snapshot, under the nginx body limit.
	MaxTotalBytes = 24 << 20
)

// ErrPath means the relative path leaves the session directory.
var ErrPath = errors.New("путь запрещён")

// ErrTooLarge means the snapshot exceeds the file or byte limit.
var ErrTooLarge = errors.New("слишком большой проект")

// File is one relative path in a project snapshot or a change list.
type File struct {
	Path     string `json:"path"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
}

// Snapshot is the project the client sends at the start of a session.
type Snapshot struct {
	Files   []File
	Skipped []string
}

// SafePath resolves rel inside root. It rejects absolute paths, "..", and ".git".
func SafePath(root, rel string) (string, error) {
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	if rel == "" || strings.HasPrefix(rel, "/") || path.IsAbs(rel) {
		return "", ErrPath
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrPath
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".git" || seg == "" || seg == "." || seg == ".." {
			return "", ErrPath
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(absRoot, filepath.FromSlash(cleaned))
	back, err := filepath.Rel(absRoot, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", ErrPath
	}
	if err := staysInside(absRoot, full); err != nil {
		return "", err
	}
	return full, nil
}

// staysInside rejects a symlink whose target leaves root. The lexical path
// can stay inside the root while ReadFile and WriteFile follow the link out.
func staysInside(absRoot, full string) error {
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return err
	}
	suffix := ""
	current := full
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return err
			}
			candidate := resolved
			if suffix != "" {
				candidate = filepath.Join(resolved, suffix)
			}
			rel, err := filepath.Rel(resolvedRoot, candidate)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return ErrPath
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if current == resolvedRoot || current == absRoot || current == filepath.Dir(current) {
			return ErrPath
		}
		base := filepath.Base(current)
		if suffix == "" {
			suffix = base
		} else {
			suffix = filepath.Join(base, suffix)
		}
		current = filepath.Dir(current)
	}
}

// Bytes returns the file contents.
func (f File) Bytes() ([]byte, error) {
	if f.Encoding == "" {
		return []byte(f.Content), nil
	}
	if f.Encoding != "base64" {
		return nil, ErrPath
	}
	return base64.StdEncoding.DecodeString(f.Content)
}

// FileFrom builds a File from raw bytes.
func FileFrom(rel string, body []byte) File {
	rel = filepath.ToSlash(rel)
	if utf8.Valid(body) {
		return File{Path: rel, Content: string(body)}
	}
	return File{Path: rel, Content: base64.StdEncoding.EncodeToString(body), Encoding: "base64"}
}

// Replace makes the project files inside root match files.
// A .git file or directory is left in place so a linked worktree stays valid.
func Replace(root string, files []File) error {
	if len(files) > MaxFiles {
		return ErrTooLarge
	}
	want := map[string]File{}
	total := 0
	for _, f := range files {
		if f.Deleted {
			continue
		}
		if _, err := SafePath(root, f.Path); err != nil {
			return err
		}
		body, err := f.Bytes()
		if err != nil {
			return err
		}
		if len(body) > MaxFileBytes {
			return ErrTooLarge
		}
		total += len(body)
		if total > MaxTotalBytes {
			return ErrTooLarge
		}
		key := path.Clean(strings.ReplaceAll(f.Path, "\\", "/"))
		want[key] = f
	}
	for key, f := range want {
		dest, err := SafePath(root, key)
		if err != nil {
			return err
		}
		body, err := f.Bytes()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return err
		}
	}
	return removeMissing(root, want)
}

// Apply writes a change list. Deletions remove only files.
func Apply(root string, files []File) error {
	for _, f := range files {
		dest, err := SafePath(root, f.Path)
		if err != nil {
			return err
		}
		if f.Deleted {
			if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		body, err := f.Bytes()
		if err != nil {
			return err
		}
		if len(body) > MaxFileBytes {
			return ErrTooLarge
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// HashTree returns sha256 hex of every file except anything inside .git.
func HashTree(root string) (map[string]string, error) {
	out := map[string]string{}
	err := walkFiles(root, func(rel string, body []byte) error {
		sum := sha256.Sum256(body)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	return out, err
}

// Diff lists files whose contents differ from baseline, including deletions.
func Diff(root string, baseline map[string]string) ([]File, error) {
	if baseline == nil {
		baseline = map[string]string{}
	}
	current, err := HashTree(root)
	if err != nil {
		return nil, err
	}
	changed := []File{}
	total := 0
	for rel, sum := range current {
		if baseline[rel] == sum {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if len(changed) >= MaxFiles || total+len(body) > MaxTotalBytes {
			return nil, ErrTooLarge
		}
		total += len(body)
		changed = append(changed, FileFrom(rel, body))
	}
	for rel := range baseline {
		if _, ok := current[rel]; ok {
			continue
		}
		if len(changed) >= MaxFiles {
			return nil, ErrTooLarge
		}
		changed = append(changed, File{Path: rel, Deleted: true})
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })
	return changed, nil
}

// Collect reads a local project. Gitignored files, dependency directories,
// symlinks, and files larger than MaxFileBytes are skipped.
func Collect(root string) (Snapshot, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, err
	}
	var rels []string
	err = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) && p != abs {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	ignored := gitIgnored(abs, rels)
	var snap Snapshot
	total := 0
	for _, rel := range rels {
		if ignored[rel] || !keepRel(rel) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(rel)))
		if err != nil {
			return Snapshot{}, err
		}
		if len(body) > MaxFileBytes {
			snap.Skipped = append(snap.Skipped, rel)
			continue
		}
		if len(snap.Files) >= MaxFiles || total+len(body) > MaxTotalBytes {
			return Snapshot{}, ErrTooLarge
		}
		total += len(body)
		snap.Files = append(snap.Files, FileFrom(rel, body))
	}
	return snap, nil
}

func keepRel(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".git" || seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return rel != "" && !strings.HasPrefix(rel, "/")
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func removeMissing(root string, want map[string]File) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".git" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := want[rel]; ok {
			return nil
		}
		return os.Remove(p)
	})
}

func walkFiles(root string, fn func(rel string, body []byte) error) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".git" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), body)
	})
}

func gitIgnored(root string, rels []string) map[string]bool {
	out := map[string]bool{}
	if len(rels) == 0 {
		return out
	}
	var stdin bytes.Buffer
	for _, rel := range rels {
		stdin.WriteString(rel)
		stdin.WriteByte(0)
	}
	cmd := exec.Command("git", "-C", root, "check-ignore", "--stdin", "-z")
	cmd.Stdin = &stdin
	raw, err := cmd.Output()
	if len(raw) == 0 {
		return out
	}
	// Exit status 1 means nothing was ignored. Output still wins when present.
	if err != nil && len(raw) == 0 {
		return out
	}
	for _, part := range bytes.Split(raw, []byte{0}) {
		if len(part) == 0 {
			continue
		}
		out[filepath.ToSlash(string(part))] = true
	}
	return out
}
