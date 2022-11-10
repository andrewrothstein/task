package execext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/shell"
	"mvdan.cc/sh/v3/syntax"
)

// RunCommandOptions is the options for the RunCommand func
type RunCommandOptions struct {
	Command   string
	Dir       string
	Env       []string
	POSIXOpts []string
	BashOpts  []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

var (
	// ErrNilOptions is returned when a nil options is given
	ErrNilOptions = errors.New("execext: nil options given")
)

// RunCommand runs a shell command
func RunCommand(ctx context.Context, opts *RunCommandOptions) error {
	if opts == nil {
		return ErrNilOptions
	}

	// Set "-e" or "errexit" by default
	opts.POSIXOpts = append(opts.POSIXOpts, "e")

	// Format POSIX options into a slice that mvdan/sh understands
	var params []string
	for _, opt := range opts.POSIXOpts {
		if len(opt) == 1 {
			params = append(params, fmt.Sprintf("-%s", opt))
		} else {
			params = append(params, "-o")
			params = append(params, opt)
		}
	}

	var s []string

	// Manually adding bash opts before each command as there is no interp.Shopts
	if len(opts.BashOpts) > 0 {
		s = append(s, fmt.Sprintf("shopt -s %s", strings.Join(opts.BashOpts, " ")))
	}

	if len(s) > 0 {
		opts.Command = strings.Join(append(s, opts.Command), " && ")
	}

	p, err := syntax.NewParser().Parse(strings.NewReader(opts.Command), "")
	if err != nil {
		return err
	}

	environ := opts.Env
	if len(environ) == 0 {
		environ = os.Environ()
	}

	r, err := interp.New(
		interp.Params(params...),
		interp.Env(expand.ListEnviron(environ...)),
		interp.ExecHandler(interp.DefaultExecHandler(15*time.Second)),
		interp.OpenHandler(openHandler),
		interp.StdIO(opts.Stdin, opts.Stdout, opts.Stderr),
		dirOption(opts.Dir),
	)
	if err != nil {
		return err
	}

	return r.Run(ctx, p)
}

// IsExitError returns true the given error is an exis status error
func IsExitError(err error) bool {
	if _, ok := interp.IsExitStatus(err); ok {
		return true
	}
	return false
}

// Expand is a helper to mvdan.cc/shell.Fields that returns the first field
// if available.
func Expand(s string) (string, error) {
	s = filepath.ToSlash(s)
	s = strings.ReplaceAll(s, " ", `\ `)
	fields, err := shell.Fields(s, nil)
	if err != nil {
		return "", err
	}
	if len(fields) > 0 {
		return fields[0], nil
	}
	return "", nil
}

func openHandler(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
	if path == "/dev/null" {
		return devNull{}, nil
	}
	return interp.DefaultOpenHandler()(ctx, path, flag, perm)
}

func dirOption(path string) interp.RunnerOption {
	return func(r *interp.Runner) error {
		err := interp.Dir(path)(r)
		if err == nil {
			return nil
		}

		// If the specified directory doesn't exist, it will be created later.
		// Therefore, even if `interp.Dir` method returns an error, the
		// directory path should be set only when the directory cannot be found.
		if absPath, _ := filepath.Abs(path); absPath != "" {
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				r.Dir = absPath
				return nil
			}
		}

		return err
	}
}
