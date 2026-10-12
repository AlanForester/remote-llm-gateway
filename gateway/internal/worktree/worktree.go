// Package worktree prepares an isolated directory for one user and project.
package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Prepare returns the server directory for this user and workspace.
// When workspace is a git repository on this machine, the directory is a
// detached worktree. Otherwise it is an empty directory keyed by the path.
func Prepare(root, userID, workspace string) (string, error) {
	if root == "" {
		root = "data/workspaces"
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(workspace)))
	name := sanitize(userID) + "-" + hex.EncodeToString(sum[:6])
	dest := filepath.Join(root, name)
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		return dest, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if workspace != "" && isGitRepo(workspace) {
		cmd := exec.Command("git", "-C", workspace, "worktree", "add", "--detach", dest)
		if err := cmd.Run(); err == nil {
			return dest, nil
		}
		_ = os.RemoveAll(dest)
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", err
	}
	return dest, nil
}

func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "user"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "user"
	}
	return b.String()
}
