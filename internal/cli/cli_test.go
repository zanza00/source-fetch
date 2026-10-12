package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs f with os.Stdout redirected and returns what was printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = old
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestVersionCommand(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = Run([]string{"version"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out); got != Version {
		t.Fatalf("version output = %q, want %q", got, Version)
	}
}

func TestUnknownOptionFails(t *testing.T) {
	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	code := Run([]string{"--nope"})
	_ = w.Close()
	os.Stderr = old
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
