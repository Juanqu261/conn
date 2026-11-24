// Package kirospec reads a local Kiro specification only to derive the
// provenance Conn stores for an imported Objective: a bounded SHA-256 digest
// and a safe relative reference. Source bytes never leave this process.
package kirospec

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxSourceBytes bounds a source file read solely to calculate its digest.
const MaxSourceBytes int64 = 1 << 20

// Hash returns the lowercase hexadecimal SHA-256 digest of a regular,
// non-symlink file no larger than MaxSourceBytes.
func Hash(path string) (string, error) {
	digest, _, err := read(path, false)
	return digest, err
}

// HashAndTitle also returns the text of the file's first Markdown level-one
// heading, as a suggestion a human reviews before it becomes an Objective title.
func HashAndTitle(path string) (string, string, error) {
	return read(path, true)
}

func read(path string, wantTitle bool) (string, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", fmt.Errorf("read Kiro specification: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", errors.New("Kiro specification source must be a regular non-symlink file")
	}
	if info.Size() > MaxSourceBytes {
		return "", "", errors.New("Kiro specification source is larger than 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("read Kiro specification: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() > MaxSourceBytes {
		return "", "", errors.New("Kiro specification source must remain a regular file no larger than 1 MiB")
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read Kiro specification: %w", err)
	}
	if int64(len(content)) > MaxSourceBytes {
		return "", "", errors.New("Kiro specification source is larger than 1 MiB")
	}
	title := ""
	if wantTitle {
		title = firstHeading(content)
	}
	return fmt.Sprintf("%x", sha256.Sum256(content)), title, nil
}

func firstHeading(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if heading, ok := strings.CutPrefix(line, "# "); ok {
			heading = strings.TrimSpace(heading)
			if utf8.RuneCountInString(heading) > 200 {
				heading = string([]rune(heading)[:200])
			}
			return heading
		}
	}
	return ""
}

// SafeReference normalizes a relative source reference to forward slashes
// and rejects absolute or traversing references.
func SafeReference(value string) (string, error) {
	normalized := strings.ReplaceAll(value, `\`, "/")
	if normalized == "" || filepath.IsAbs(value) || strings.HasPrefix(normalized, "/") {
		return "", errors.New("the source reference must be a relative path")
	}
	clean := filepath.ToSlash(filepath.Clean(normalized))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("the source reference must not traverse outside its local specification")
	}
	return clean, nil
}
