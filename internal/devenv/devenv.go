// Package devenv loads local development settings from a .env file.
//
// It exists for convenience during development only, and its scope is
// deliberately narrow:
//
//   - only keys starting with CONN_ are read, so another project's .env
//     secrets are never picked up when Kiro starts conn in that repository;
//   - variables already set in the environment always win;
//   - CONN_TOKEN is refused, because session credentials never live in files;
//   - empty values are skipped and leave the variable unset.
//
// The format is KEY=VALUE per line, with optional "export " and optional
// matching quotes around the value. Lines starting with # are comments.
// Inline comments are not supported, because values such as URLs may
// contain #.
package devenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// FileName is the settings file read from the working directory.
const FileName = ".env"

const (
	prefix   = "CONN_"
	tokenKey = "CONN_TOKEN"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Result describes what a load did. It never contains values.
type Result struct {
	Path       string
	Loaded     []string // keys set from the file
	Overridden []string // keys already set in the environment, left unchanged
	Warnings   []string
}

// Load applies FileName from the working directory. A missing file is not
// an error and returns nil.
func Load() (*Result, error) {
	return LoadFile(FileName, os.LookupEnv, os.Setenv)
}

// LoadFile applies the settings in path through lookup and set.
func LoadFile(path string, lookup func(string) (string, bool), set func(string, string) error) (*Result, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	result := &Result{Path: path}
	entries, warnings, err := parse(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	result.Warnings = warnings
	for _, entry := range entries {
		switch {
		case entry.key == tokenKey:
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"%s line %d: ignored %s; session credentials must never be stored in files", path, entry.line, tokenKey))
		case entry.value == "":
		default:
			if _, exists := lookup(entry.key); exists {
				result.Overridden = append(result.Overridden, entry.key)
				continue
			}
			if err := set(entry.key, entry.value); err != nil {
				return nil, fmt.Errorf("set %s from %s: %w", entry.key, path, err)
			}
			result.Loaded = append(result.Loaded, entry.key)
		}
	}
	return result, nil
}

type entry struct {
	line       int
	key, value string
}

// parse returns the CONN_ entries of a .env file and warnings for malformed
// lines. Warnings name the line, never its value.
func parse(r io.Reader) ([]entry, []string, error) {
	var entries []entry
	var warnings []string
	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if line == 1 {
			text = strings.TrimPrefix(text, "\ufeff")
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || !keyPattern.MatchString(key) {
			warnings = append(warnings, fmt.Sprintf("%s line %d: expected KEY=VALUE; line ignored", FileName, line))
			continue
		}
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		entries = append(entries, entry{line: line, key: key, value: value})
	}
	return entries, warnings, scanner.Err()
}
