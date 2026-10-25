package filesync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReplaceKeepsGitAndDropsStaleFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /somewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Replace(root, []File{{Path: "src/main.txt", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "src", "main.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("written %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file still present: %v", err)
	}
	git, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil || string(git) == "" {
		t.Fatalf("git link: %q %v", git, err)
	}
}

func TestSafePathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../x", "/etc/passwd", ".git/config", "a/../../b", `..\x`} {
		if _, err := SafePath(root, rel); err != ErrPath {
			t.Fatalf("%s: %v", rel, err)
		}
	}
}

func TestSafePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"leak", "dirlink/secret.txt"} {
		if _, err := SafePath(root, rel); err != ErrPath {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	if err := Apply(root, []File{{Path: "dirlink/secret.txt", Content: "overwrite"}}); err != ErrPath {
		t.Fatalf("apply %v", err)
	}
	body, err := os.ReadFile(secret)
	if err != nil || string(body) != "hidden" {
		t.Fatalf("outside file changed: %q %v", body, err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}
	if _, err := SafePath(root, "inside/note.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestDiffAfterEdit(t *testing.T) {
	root := t.TempDir()
	if err := Replace(root, []File{{Path: "a.txt", Content: "one"}}); err != nil {
		t.Fatal(err)
	}
	base, err := HashTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	// Recreate a.txt as deleted from the tree: already removed.
	changed, err := Diff(root, base)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]File{}
	for _, f := range changed {
		got[f.Path] = f
	}
	if !got["a.txt"].Deleted {
		t.Fatalf("a.txt: %+v", got["a.txt"])
	}
	if got["b.txt"].Content != "new" || got["b.txt"].Deleted {
		t.Fatalf("b.txt: %+v", got["b.txt"])
	}
}

func TestBinaryRoundTrip(t *testing.T) {
	body := []byte{0xff, 0xfe, 0x00, 0x01}
	f := FileFrom("bin.dat", body)
	if f.Encoding != "base64" {
		t.Fatalf("%+v", f)
	}
	back, err := f.Bytes()
	if err != nil || string(back) != string(body) {
		t.Fatalf("%q %v", back, err)
	}
}

func TestCollectSkipsGitignored(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.env"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdLook := filepath.Join(root, ".git")
	if err := os.Mkdir(cmdLook, 0o755); err != nil {
		t.Fatal(err)
	}
	// git check-ignore needs a repo. init is enough; the file does not have to be committed.
	if err := runGit(root, "init"); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(root)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range snap.Files {
		names[f.Path] = true
	}
	if !names["keep.txt"] || !names[".gitignore"] {
		t.Fatalf("files: %+v", snap.Files)
	}
	if names["secret.env"] {
		t.Fatal("gitignored file was collected")
	}
}

func TestChunkSplitsBeforeRequestLimit(t *testing.T) {
	files := []File{
		{Path: "a.txt", Content: "12345"},
		{Path: "b.txt", Content: "12345"},
		{Path: "c.txt", Content: "12345"},
	}
	chunks := Chunk(files, 2, 1000)
	if len(chunks) != 2 || len(chunks[0]) != 2 || len(chunks[1]) != 1 {
		t.Fatalf("by count: %+v", chunks)
	}
	chunks = Chunk(files, 10, len(files[0].Path)+len(files[0].Content)+64+1)
	if len(chunks) != 3 {
		t.Fatalf("by size: %d", len(chunks))
	}
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_COMMITTER_NAME=test")
	return cmd.Run()
}
