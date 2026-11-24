package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestKiroSpecSourceHashingIsBoundedAndReferenceSafe(t *testing.T) {
	source := filepath.Join(t.TempDir(), "requirements.md")
	content := []byte("# Safe local specification\n")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := readKiroSpecHash(source)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(content))
	if hash != want {
		t.Fatalf("hash = %s, want %s", hash, want)
	}
	if _, err := safeSourceReference("../requirements.md"); err == nil {
		t.Fatal("traversing source reference was accepted")
	}
	ref, err := safeSourceReference(`.kiro\specs\checkout\requirements.md`)
	if err != nil || ref != ".kiro/specs/checkout/requirements.md" {
		t.Fatalf("safe reference = %q, %v", ref, err)
	}
	oversized := filepath.Join(t.TempDir(), "large.md")
	if err := os.WriteFile(oversized, make([]byte, maxImportSourceBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKiroSpecHash(oversized); err == nil {
		t.Fatal("oversized source was accepted")
	}
}
