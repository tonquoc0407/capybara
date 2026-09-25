package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard writes text to the clipboard using an OSC 52 terminal sequence
// and falls back to platform clipboard commands (pbcopy, xclip, wl-copy, clip.exe).
func copyToClipboard(text string) error {
	if text == "" {
		return nil
	}
	// 1. OSC 52 sequence writes directly to standard output for terminal-native support.
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", b64)
	if os.Getenv("TMUX") != "" {
		seq = fmt.Sprintf("\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", b64)
	}
	_, _ = os.Stdout.WriteString(seq)

	// 2. Best-effort platform clipboard command.
	copyNativeClipboard(text)
	return nil
}

func copyNativeClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}
