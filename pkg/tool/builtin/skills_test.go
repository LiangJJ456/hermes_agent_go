package builtin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDescription_Frontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: foo\ndescription: Does the foo thing.\nallowed-tools: Read\n---\n\n# foo\n\nbody line\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractDescription(path)
	if got != "Does the foo thing." {
		t.Fatalf("got %q, want the frontmatter description", got)
	}
}

func TestExtractDescription_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := "# Title\n\nFirst real line.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractDescription(path)
	if got != "First real line." {
		t.Fatalf("got %q", got)
	}
}
