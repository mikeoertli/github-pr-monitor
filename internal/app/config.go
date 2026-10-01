package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
)

func editConfig(path string, out, stderr io.Writer) error {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad.exe"
		}
	}
	args, err := editorArgs(editor)
	if err != nil {
		return err
	}
	executable, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("find editor: %w; set EDITOR to your preferred editor", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		if err = config.WriteExample(path); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	cmd := exec.Command(executable, append(args[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	c, err := config.Load(path)
	if err == nil {
		err = c.Validate()
	}
	if err != nil {
		return fmt.Errorf("config saved at %s but invalid: %w", path, err)
	}
	_, err = fmt.Fprintln(out, "Config ready: "+path+" (applies on the next launch)")
	return err
}

// Support EDITOR='code --wait' and quoted paths without invoking a shell.
// Shell operators, substitutions and environment expansion are never evaluated.
func editorArgs(raw string) ([]string, error) {
	if _, err := exec.LookPath(raw); err == nil {
		return []string{raw}, nil
	}
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range raw {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, started = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case unicode.IsSpace(r):
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("EDITOR contains an unfinished quote or escape")
	}
	if started {
		args = append(args, word.String())
	}
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("EDITOR must name an executable")
	}
	return args, nil
}
