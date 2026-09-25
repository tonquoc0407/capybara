package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"unicode"
)

// EditorCommand returns the configured editor without invoking a shell.
func EditorCommand() (string, []string, error) {
	return parseEditor(selectEditor(runtime.GOOS, os.Getenv("VISUAL"), os.Getenv("EDITOR")))
}

func selectEditor(goos, visual, editor string) string {
	if visual != "" {
		return visual
	}
	if editor != "" {
		return editor
	}
	if goos == "windows" {
		return "notepad.exe"
	}
	return "vi"
}

// parseEditor splits a command without a shell. Single/double quotes group
// spaces; backslashes stay literal, including in Windows executable paths.
// Shell expansion, pipes, and redirection are deliberately not interpreted.
func parseEditor(command string) (string, []string, error) {
	var fields []string
	var word strings.Builder
	var quote rune
	started := false
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			if started {
				fields = append(fields, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return "", nil, fmt.Errorf("editor command has an unclosed quote")
	}
	if started {
		fields = append(fields, word.String())
	}
	if len(fields) == 0 || fields[0] == "" {
		return "", nil, fmt.Errorf("editor command has no executable")
	}
	return fields[0], fields[1:], nil
}
