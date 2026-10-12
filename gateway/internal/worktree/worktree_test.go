package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPreparePlainDir(t *testing.T) {
	root := t.TempDir()
	first, err := Prepare(root, "ann", "/local/project")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(root, "ann", "/local/project")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("dirs differ: %s %s", first, second)
	}
	if st, err := os.Stat(first); err != nil || !st.IsDir() {
		t.Fatal(err)
	}
}

func TestPrepareGitWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(repo, "readme.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "readme.txt")
	run("commit", "-m", "init")
	dest, err := Prepare(t.TempDir(), "ann", repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "readme.txt")); err != nil {
		t.Fatal(err)
	}
}
