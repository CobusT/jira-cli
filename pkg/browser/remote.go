package browser

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Session describes where the user's terminal is relative to this process.
type Session struct {
	// Remote is true when the user's terminal is on another machine (mosh or
	// ssh from a non-loopback address), so opening a browser here won't reach them.
	Remote bool
	// TmuxClient is the tmux client the user is on, if running inside tmux.
	TmuxClient string
}

// DetectSession works out whether the user's terminal is remote. Inside tmux it
// checks the session's most recently active client, so a session shared by a
// local and a remote terminal follows whichever the user is typing in.
//
// JIRA_REMOTE=1 or JIRA_REMOTE=0 overrides the detection.
func DetectSession() Session {
	var s Session

	pid := os.Getppid()
	if os.Getenv("TMUX") != "" {
		out, err := exec.Command("tmux", "display-message", "-p", "#{client_name}\t#{client_pid}").Output()
		if err == nil {
			parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
			if len(parts) == 2 {
				s.TmuxClient = parts[0]
				if p, err := strconv.Atoi(parts[1]); err == nil {
					pid = p
				}
			}
		}
	}

	switch strings.ToLower(os.Getenv("JIRA_REMOTE")) {
	case "1", "true", "yes":
		s.Remote = true
		return s
	case "0", "false", "no":
		return s
	}

	s.Remote = isRemoteProcessTree(pid)
	return s
}

// isRemoteProcessTree walks up the process tree from pid looking for a mosh
// or ssh server.
func isRemoteProcessTree(pid int) bool {
	for i := 0; i < 10 && pid > 1; i++ {
		out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return false
		}
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			return false
		}
		comm := strings.Join(fields[1:], " ")

		switch {
		case strings.Contains(comm, "mosh-server"):
			return true
		case strings.Contains(comm, "sshd"):
			return !isLoopbackSSH(pid)
		}

		ppid, err := strconv.Atoi(fields[0])
		if err != nil {
			return false
		}
		pid = ppid
	}
	return false
}

// isLoopbackSSH reports whether the ssh server process is serving a connection
// from this machine (eg: a local terminal multiplexer using ssh localhost).
func isLoopbackSSH(pid int) bool {
	out, err := exec.Command("lsof", "-a", "-nP", "-iTCP", "-sTCP:ESTABLISHED", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.Index(line, "->")
		if idx < 0 {
			continue
		}
		remote := strings.Fields(line[idx+2:])[0]
		host, _, err := net.SplitHostPort(remote)
		if err != nil {
			continue
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
		return false
	}
	return false
}

// CopyToTerminalClipboard copies text to the clipboard of the user's terminal
// using the OSC 52 escape sequence, which works across ssh and mosh. Inside
// tmux, tmux sends the sequence to the given client (requires set-clipboard
// on or external, the default).
func CopyToTerminalClipboard(text string, s Session) error {
	if os.Getenv("TMUX") != "" {
		args := []string{"load-buffer", "-w"}
		if s.TmuxClient != "" {
			args = append(args, "-t", s.TmuxClient)
		}
		args = append(args, "-")

		cmd := exec.Command("tmux", args...)
		cmd.Stdin = strings.NewReader(text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("tmux load-buffer: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}

	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = tty.Close() }()

	_, err = fmt.Fprintf(tty, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}

// HasBrowserOverride reports whether JIRA_BROWSER or BROWSER is set, in which
// case the user's chosen opener is always used.
func HasBrowserOverride() bool {
	return getBrowserFromENV() != ""
}
