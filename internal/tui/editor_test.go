package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSelectEditor(t *testing.T) {
	for _, tc := range []struct {
		name, goos, visual, editor, want string
	}{
		{"visual wins", "windows", "visual --wait", "editor", "visual --wait"},
		{"editor", "linux", "", "nano", "nano"},
		{"windows default", "windows", "", "", "notepad.exe"},
		{"linux default", "linux", "", "", "vi"},
		{"macOS default", "darwin", "", "", "vi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectEditor(tc.goos, tc.visual, tc.editor); got != tc.want {
				t.Fatalf("editor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseEditor(t *testing.T) {
	for _, tc := range []struct {
		command, name string
		args          []string
	}{
		{"vi", "vi", []string{}},
		{"  code\t--wait  ", "code", []string{"--wait"}},
		{`"C:\Program Files\Editor\editor.exe" --wait`, `C:\Program Files\Editor\editor.exe`, []string{"--wait"}},
		{`'C:\Program Files\Editor\editor.exe' --wait`, `C:\Program Files\Editor\editor.exe`, []string{"--wait"}},
		{`"/tmp/editor with spaces" --title 'tool output' ""`, "/tmp/editor with spaces", []string{"--title", "tool output", ""}},
		{`C:\tools\editor.exe --title="tool output"`, `C:\tools\editor.exe`, []string{"--title=tool output"}},
		{`editor '$HOME' *.json ; echo`, "editor", []string{"$HOME", "*.json", ";", "echo"}},
		{`"/tmp/éditeur" "日本語"`, "/tmp/éditeur", []string{"日本語"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			name, args, err := parseEditor(tc.command)
			if err != nil || name != tc.name || !reflect.DeepEqual(args, tc.args) {
				t.Fatalf("parseEditor = %q, %#v, %v; want %q, %#v", name, args, err, tc.name, tc.args)
			}
		})
	}
	for _, command := range []string{"", " \t ", `"" --wait`, `"unclosed`, `'unclosed`} {
		if _, _, err := parseEditor(command); err == nil {
			t.Errorf("parseEditor(%q) should fail", command)
		}
	}
}

func TestEditorProcessHandlesPathsWithSpaces(t *testing.T) {
	if os.Getenv("CAPYBARA_TEST_EDITOR") == "1" {
		if err := os.WriteFile(os.Args[len(os.Args)-1], []byte(`{"price":99}`), 0o600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	configureTestEditor(t)
	path := filepath.Join(t.TempDir(), "tool output 日本語.json")
	name, args, err := editorCommand()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(name, append(args, path)...).CombinedOutput(); err != nil {
		t.Fatalf("editor did not run: %v: %s", err, output)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != `{"price":99}` {
		t.Fatalf("edited output = %q, %v", body, err)
	}
}

// Use the native test binary as an editor, rather than requiring a shell or a
// host editor. The helper test writes a replacement and exits in the child.
func configureTestEditor(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	// Ensure the executable path itself contains spaces/non-ASCII too, even on
	// CI hosts whose temporary build directory happens to be a simple path.
	editor := filepath.Join(t.TempDir(), "test editor 日本語"+filepath.Ext(exe))
	if err := os.WriteFile(editor, body, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPYBARA_TEST_EDITOR", "1")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", `"`+editor+`" -test.run=^TestEditorProcessHandlesPathsWithSpaces$ --`)
}

func TestInvalidEditorReturnsErrorAndCleansUp(t *testing.T) {
	t.Setenv("VISUAL", "  ")
	path := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := appModel{}
	msg, ok := m.editorCmd(editReadyMsg{path: path})().(editDoneMsg)
	if !ok || msg.err == nil {
		t.Fatalf("invalid editor must return an edit error: %+v", msg)
	}
	model, _ := m.applyEdit(msg)
	if model.(appModel).lastErr == nil {
		t.Fatal("editor error must reach the UI")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("staged output was not removed: %v", err)
	}
}
