package git

import "testing"

func TestValidateRef(t *testing.T) {
	valid := []string{
		"", // empty means the remote default branch
		"main",
		"master",
		"v1.0.0",
		"release/1.0",
		"refs/tags/v1",
		"feature/foo+bar",
		"abc123",
		"user@branch",
		"a.b/c-d",
	}
	for _, ref := range valid {
		t.Run("valid_"+ref, func(t *testing.T) {
			if err := ValidateRef(ref); err != nil {
				t.Fatalf("ValidateRef(%q) = %v, want nil", ref, err)
			}
		})
	}

	invalid := []string{
		"-main",
		"--upload-pack=evil",
		".",
		"..",
		"main..x",
		"...",
		"/main",
		"main/",
		"a//b",
		".hidden",
		"a/.hidden",
		"main.lock",
		"a/main.lock",
		"main.",
		"@",
		"main@{1}",
		"main branch",
		"main\tbranch",
		"main\nbranch",
		"main\rbranch",
		"main\x00branch",
		"main~1",
		"main^",
		"main:foo",
		"main?",
		"main*",
		"main[",
		`main\foo`,
	}
	for _, ref := range invalid {
		t.Run("invalid_"+ref, func(t *testing.T) {
			if err := ValidateRef(ref); err == nil {
				t.Fatalf("ValidateRef(%q) = nil, want an error", ref)
			}
		})
	}
}
