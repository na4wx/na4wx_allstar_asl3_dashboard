// System page "Check for updates" -- compares this app's own git
// checkout against its upstream branch and, if asked, re-runs
// install.sh to pull and rebuild, streaming its output live to the
// browser over a dedicated WebSocket. Both halves are best-effort and
// gracefully absent (not an error page) when repoDir can't be found --
// a device deployed via deploy/install.sh's own binary-only path
// (cross-compiled elsewhere, copied over) never had a git checkout on
// it in the first place, and even one deployed via the top-level
// install.sh only gets the marker this reads once that script has
// actually run at least once (see its own comment on writing it).
//
// Running the update this way restarts this very process partway
// through (install.sh's own last step is `systemctl restart
// asl3-gui`) -- the stream (and the WebSocket carrying it) necessarily
// dies at that exact moment along with the rest of this process. That
// is expected, not a bug: the client side treats a stream close after
// a successful run as "the update finished and the service is
// restarting", not a failure, and polls for the app to come back
// before offering to reload.
package server

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// repoDirMarkerPath is written by install.sh (top-level) once it's run
// successfully -- see that script's own comment on why it's written
// early, unconditionally, before anything else can fail.
const repoDirMarkerPath = "/etc/asl3-gui/repo-dir"

// updateGitTimeout bounds a single git operation (fetch, rev-list,
// log) -- generous for a fetch over a slow connection, but short enough
// that a stuck git process doesn't hang the whole status check.
const updateGitTimeout = 30 * time.Second

// repoDir reads the marker install.sh writes, trimmed of whitespace. An
// empty result (missing file, or a file that's somehow empty) means
// "not configured" -- callers treat that as "update checking
// unavailable", never as an error to surface loudly.
func repoDir() string {
	b, err := os.ReadFile(repoDirMarkerPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

type updateStatus struct {
	Available bool     `json:"available"`
	Branch    string   `json:"branch,omitempty"`
	UpToDate  bool     `json:"upToDate"`
	Behind    int      `json:"behind"`
	Commits   []string `json:"commits,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// runGit runs git -C dir <args> under updateGitTimeout, returning
// trimmed stdout. Always passes -c safe.directory=dir -- this process
// runs as root (see the systemd unit), but the checkout was normally
// cloned by whatever regular user ran install.sh by hand, so without
// this every git call here fails outright with git's own "dubious
// ownership" refusal (exit 128) the moment root touches a repo it
// doesn't own. Scoped to just this one invocation via -c rather than
// writing to root's own ~/.gitconfig, so this never depends on (or
// mutates) any persistent config -- see install.sh's own comment for
// why *it* still needs a persistent version of this same fix.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, updateGitTimeout)
	defer cancel()
	fullArgs := append([]string{"-c", "safe.directory=" + dir, "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// checkForUpdatesInDir mirrors install.sh's own "already up to date?"
// check exactly (fetch, then compare HEAD against @{u} on whatever
// branch is currently checked out) -- this is a read-only report,
// never itself changing anything on disk. Takes dir directly (rather
// than resolving repoDir() itself) so it's testable against a real
// throwaway git repo without touching repoDirMarkerPath on the real
// filesystem; handleUpdateCheck resolves repoDir() once and passes it
// in.
func checkForUpdatesInDir(ctx context.Context, dir string) updateStatus {
	if dir == "" {
		return updateStatus{Available: false}
	}

	if _, err := runGit(ctx, dir, "fetch", "origin"); err != nil {
		return updateStatus{Available: true, Error: "couldn't reach the git remote: " + err.Error()}
	}

	branch, err := runGit(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return updateStatus{Available: true, Error: "couldn't determine the current branch: " + err.Error()}
	}

	countStr, err := runGit(ctx, dir, "rev-list", "--count", "HEAD..@{u}")
	if err != nil {
		return updateStatus{Available: true, Branch: branch, Error: "branch " + branch + " has no upstream to compare against"}
	}
	behind, _ := strconv.Atoi(countStr)
	if behind == 0 {
		return updateStatus{Available: true, Branch: branch, UpToDate: true}
	}

	log, _ := runGit(ctx, dir, "log", "--oneline", "-n", "20", "HEAD..@{u}")
	var commits []string
	if log != "" {
		commits = strings.Split(log, "\n")
	}
	return updateStatus{Available: true, Branch: branch, UpToDate: false, Behind: behind, Commits: commits}
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, checkForUpdatesInDir(r.Context(), repoDir()))
}

// updateStreamMsg is the one message shape sent over
// GET /system/update/run -- "line" for each chunk of install.sh's own
// combined stdout/stderr as it runs, then exactly one final "done" (OK
// true) or "error" (OK false, Line holds the failure reason) before the
// connection closes. A close with no final message at all (see this
// file's own doc comment) means the update most likely succeeded and
// this process is mid-restart -- the client treats that case the same
// as a "done" it never got to see.
type updateStreamMsg struct {
	Type string `json:"type"`
	Line string `json:"line,omitempty"`
	OK   bool   `json:"ok,omitempty"`
}

// handleUpdateStream re-runs install.sh in repoDir and streams its
// combined output live. Deliberately NOT tied to the request context
// once started: closing the browser tab mid-update must never kill a
// git pull / rebuild partway through and leave the checkout in a
// half-updated state -- the command runs to completion (or failure) on
// its own regardless of whether anything is still listening, exactly
// like running it directly at a terminal and disconnecting.
func (s *Server) handleUpdateStream(w http.ResponseWriter, r *http.Request) {
	dir := repoDir()
	if dir == "" {
		http.Error(w, "no repo directory configured -- run install.sh at least once from a real checkout first", http.StatusPreconditionFailed)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	send := func(msg updateStreamMsg) bool {
		writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return wsjson.Write(writeCtx, conn, msg) == nil
	}

	cmd := exec.Command("./install.sh")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	pr, pw, err := os.Pipe()
	if err != nil {
		send(updateStreamMsg{Type: "error", Line: "couldn't start: " + err.Error()})
		return
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		send(updateStreamMsg{Type: "error", Line: "couldn't start install.sh: " + err.Error()})
		return
	}
	pw.Close() // this process's own copy of the write end -- cmd still holds one until it exits

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		// Best-effort: a write failure here (browser tab closed, network
		// blip) doesn't stop the scan -- install.sh keeps running to
		// completion either way, see this handler's own doc comment.
		send(updateStreamMsg{Type: "line", Line: scanner.Text()})
	}
	pr.Close()

	waitErr := cmd.Wait()
	if waitErr != nil {
		send(updateStreamMsg{Type: "error", Line: waitErr.Error()})
		return
	}
	// A successful run's own last step restarts this service -- there's
	// a real race between this send and that restart actually landing,
	// so the client can't rely on ever seeing it (see this file's own
	// doc comment). Sent anyway for the common case where the restart
	// takes a moment longer than this one small write.
	send(updateStreamMsg{Type: "done", OK: true})
}
