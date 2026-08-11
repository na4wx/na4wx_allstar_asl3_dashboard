package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGitT runs git for the fixture-building steps below, failing the
// test on any error -- separate from the package's own runGit (which
// returns stdout for a caller to use) since these calls are purely
// setup and their output is never needed. dir == "" runs in the test
// process's own working directory (fine for `git clone <url> <dest>`,
// which doesn't care what directory it's invoked from).
func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"HOME="+t.TempDir(), // never touch a real ~/.gitconfig
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// newTestRepoPair builds a bare "remote" repo plus a real clone of it,
// with one commit already pushed and checked out -- the minimum
// checkForUpdatesInDir needs to have a real upstream (@{u}) to compare
// against, the same as a real install.sh checkout has against GitHub.
// Returns both directories: localDir is what tests point
// checkForUpdatesInDir at; remoteDir is what a test simulating "origin
// moved on" pushes a further commit to.
func newTestRepoPair(t *testing.T) (localDir, remoteDir string) {
	t.Helper()
	remoteDir = t.TempDir()
	runGitT(t, remoteDir, "init", "--bare", "-b", "main")

	seedDir := t.TempDir()
	runGitT(t, seedDir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(seedDir, "README"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, seedDir, "add", "README")
	runGitT(t, seedDir, "commit", "-m", "initial")
	runGitT(t, seedDir, "remote", "add", "origin", remoteDir)
	runGitT(t, seedDir, "push", "origin", "main")

	localDir = t.TempDir()
	runGitT(t, "", "clone", remoteDir, localDir)
	return localDir, remoteDir
}

func TestCheckForUpdatesUpToDate(t *testing.T) {
	dir, _ := newTestRepoPair(t)
	st := checkForUpdatesInDir(context.Background(), dir)
	if !st.Available {
		t.Fatal("Available = false, want true")
	}
	if st.Error != "" {
		t.Fatalf("Error = %q, want none", st.Error)
	}
	if !st.UpToDate {
		t.Errorf("UpToDate = false, want true (fresh clone, nothing pushed since)")
	}
	if st.Behind != 0 {
		t.Errorf("Behind = %d, want 0", st.Behind)
	}
}

// TestCheckForUpdatesBehind pushes one more commit to the same remote
// used to seed the local clone, from a second, separate clone -- never
// touching dir itself, the same as a real device's checkout not
// changing on its own just because GitHub gained a new commit. Direct
// regression case for the actual reason this feature exists: telling
// an operator their device is behind.
func TestCheckForUpdatesBehind(t *testing.T) {
	dir, remoteDir := newTestRepoPair(t)

	pushDir := t.TempDir()
	runGitT(t, "", "clone", remoteDir, pushDir)
	if err := os.WriteFile(filepath.Join(pushDir, "README"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, pushDir, "commit", "-am", "second commit")
	runGitT(t, pushDir, "push", "origin", "main")

	st := checkForUpdatesInDir(context.Background(), dir)
	if st.UpToDate {
		t.Fatal("UpToDate = true, want false -- origin has a commit this checkout doesn't")
	}
	if st.Behind != 1 {
		t.Errorf("Behind = %d, want 1", st.Behind)
	}
	if len(st.Commits) != 1 || !strings.Contains(st.Commits[0], "second commit") {
		t.Errorf("Commits = %v, want one entry mentioning %q", st.Commits, "second commit")
	}
}

func TestCheckForUpdatesUnconfigured(t *testing.T) {
	st := checkForUpdatesInDir(context.Background(), "")
	if st.Available {
		t.Error("Available = true for a blank repo dir, want false")
	}
}
