package tui

import (
	"bytes"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Launch starts prog with the file path appended, without waiting for it to
// exit. A program given as a full command line (e.g. "firefox --new-tab") is
// split into words first.
func Launch(prog, path string) error {
	argv := splitCommand(prog)
	if len(argv) == 0 {
		return errNoProgram
	}
	argv = append(argv, path)
	cmd := exec.Command(argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		if stderr.Len() > 0 {
			return errString(strings.TrimSpace(stderr.String()))
		}
		return err
	}
	// Reap the child in the background so it does not become a zombie, but do
	// not block the UI on it.
	go func() { _ = cmd.Wait() }()
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

var errNoProgram = errString("no program given")

// splitCommand splits a command line into words, honouring single and double
// quotes so that `open -a "My App"` works.
func splitCommand(s string) []string {
	var (
		out   []string
		cur   strings.Builder
		quote rune
		have  bool
	)
	flush := func() {
		if have {
			out = append(out, cur.String())
			cur.Reset()
			have = false
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			have = true
		case r == '\'' || r == '"':
			quote = r
			have = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	flush()
	return out
}

func execCommand(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

func writeTemp(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func removeFile(path string) { os.Remove(path) }

func lookPath(name string) (string, error) { return exec.LookPath(name) }

// osUserHomeDir is os.UserHomeDir, kept behind a shim so view.go needs no os
// import of its own.
func osUserHomeDir() (string, error) { return os.UserHomeDir() }

// sortStrings sorts in place.
func sortStrings(s []string) { sort.Strings(s) }
