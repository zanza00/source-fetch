package resolve

import (
	"runtime"
	"testing"

	"github.com/zanza00/source-fetch/internal/manifest"
)

func TestInstalledVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	e := &manifest.Entry{VersionCmd: "echo 1.2.3"}
	if got := InstalledVersion(e); got != "1.2.3" {
		t.Fatalf("InstalledVersion = %q, want 1.2.3", got)
	}
	// First X.Y.Z only.
	e2 := &manifest.Entry{VersionCmd: "printf 'note 9.9.9 then 1.2.3'"}
	if got := InstalledVersion(e2); got != "9.9.9" {
		t.Fatalf("first match = %q, want 9.9.9", got)
	}
	// Missing command is skipped.
	e3 := &manifest.Entry{VersionCmd: "definitely-not-a-real-bin --version"}
	if got := InstalledVersion(e3); got != "" {
		t.Fatalf("missing command = %q, want empty", got)
	}
	// Empty command.
	if got := InstalledVersion(&manifest.Entry{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestRefPrecedence(t *testing.T) {
	base := &manifest.Entry{Name: "x"}

	if got := Ref(base, "cli-ref"); got != "cli-ref" {
		t.Fatalf("cli ref wins: %q", got)
	}

	pinned := &manifest.Entry{Name: "x", Ref: "v9.9.9", VersionCmd: "echo 1.2.3"}
	if got := Ref(pinned, ""); got != "v9.9.9" {
		t.Fatalf("manifest ref wins over version: %q", got)
	}

	if runtime.GOOS != "windows" {
		versioned := &manifest.Entry{Name: "x", VersionCmd: "echo 1.2.3", TagPrefix: "v"}
		if got := Ref(versioned, ""); got != "v1.2.3" {
			t.Fatalf("version tag: %q", got)
		}
	}

	fallback := &manifest.Entry{Name: "x", DefaultRef: "stable"}
	if got := Ref(fallback, ""); got != "stable" {
		t.Fatalf("default_ref: %q", got)
	}

	if got := Ref(&manifest.Entry{Name: "x"}, ""); got != "" {
		t.Fatalf("default branch: %q", got)
	}
}
