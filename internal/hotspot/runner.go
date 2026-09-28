package hotspot

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner runs system commands, or prints them when DryRun is set.
type Runner struct {
	DryRun bool
	Out    io.Writer
	Err    io.Writer
}

// NewRunner builds a Runner writing user-visible output to out and errw.
func NewRunner(dryRun bool, out, errw io.Writer) *Runner {
	return &Runner{DryRun: dryRun, Out: out, Err: errw}
}

// Run executes name with args, feeding stdin when it is non-empty.
func (r *Runner) Run(stdin, name string, args ...string) error {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if r.DryRun {
		fmt.Fprintf(r.Out, "  + %s\n", line)
		if stdin != "" {
			for _, l := range strings.Split(strings.TrimRight(stdin, "\n"), "\n") {
				fmt.Fprintf(r.Out, "      | %s\n", l)
			}
		}
		return nil
	}
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = r.Out
	cmd.Stderr = r.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", line, err)
	}
	return nil
}

// writeFile writes content to path, honouring DryRun.
func (r *Runner) writeFile(path, content string, mode os.FileMode) error {
	if r.DryRun {
		fmt.Fprintf(r.Out, "  + write %s (%d bytes, mode %#o)\n", path, len(content), mode)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

// silent returns a Runner that discards all output, used for best-effort undo.
func (r *Runner) silent() *Runner {
	return &Runner{DryRun: r.DryRun, Out: io.Discard, Err: io.Discard}
}

// output runs a read-only command and returns its trimmed stdout.
func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// Exists reports whether an executable is present in PATH.
func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runCmd executes name with args, discarding all output. It is used for
// best-effort runtime management (nft elements, hostapd ACL) where the caller
// only cares about success/failure.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
