// Package worktree prepares an isolated directory for one user and project.
package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// Prepare returns the server directory for this user and workspace.
// The workspace string is only a key. It is not opened on the server, so a
// client cannot point the session at an existing repository.
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
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", err
	}
	return dest, nil
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
