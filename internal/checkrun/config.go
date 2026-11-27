// Package checkrun runs the reviewed check commands of a repository's
// .conn/checks.json and turns each run into observed evidence. Only the
// explicit `conn evidence run` command uses it; MCP tools and hooks never run
// checks. Commands run as argument arrays without a shell, inside the
// repository, with a timeout and without any CONN_ variable. Raw output stays
// in a bounded local log; only its hash is sent to Conn.
package checkrun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Juanqu261/conn/internal/contracts"
	"github.com/Juanqu261/conn/internal/protocol"
)

// ConfigPath is the repository-relative check configuration file.
const ConfigPath = ".conn/checks.json"

const maxConfigBytes = 256 << 10

// Config is a validated check configuration and the hash of its exact bytes.
type Config struct {
	Document protocol.ChecksConfig
	SHA256   string
}

// ConfigError is a check configuration that cannot be used. Nothing runs.
type ConfigError struct{ Message string }

func (e *ConfigError) Error() string { return e.Message }

// LoadConfig reads and validates root/.conn/checks.json against the
// checks-config schema.
func LoadConfig(root string) (Config, error) {
	path := filepath.Join(root, filepath.FromSlash(ConfigPath))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, &ConfigError{Message: ConfigPath + " does not exist; add the repository's reviewed checks before running one"}
	}
	if err != nil {
		return Config{}, &ConfigError{Message: "read " + ConfigPath + ": " + err.Error()}
	}
	if !info.Mode().IsRegular() {
		return Config{}, &ConfigError{Message: ConfigPath + " must be a regular file"}
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, &ConfigError{Message: "read " + ConfigPath + ": " + err.Error()}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, &ConfigError{Message: "read " + ConfigPath + ": " + err.Error()}
	}
	if len(data) > maxConfigBytes {
		return Config{}, &ConfigError{Message: ConfigPath + " is larger than 256 KiB"}
	}
	return ParseConfig(data)
}

// ParseConfig validates check configuration bytes.
func ParseConfig(data []byte) (Config, error) {
	// The reserved prefix is checked before the schema, whose property-name
	// failure some validators report only at the document root.
	var loose struct {
		Checks map[string]struct {
			Env map[string]json.RawMessage `json:"env"`
		} `json:"checks"`
		PairVerification struct {
			Setup struct {
				Env map[string]json.RawMessage `json:"env"`
			} `json:"setup"`
		} `json:"pair_verification"`
	}
	if json.Unmarshal(data, &loose) == nil {
		for _, name := range sortedKeys(loose.PairVerification.Setup.Env) {
			if strings.HasPrefix(strings.ToUpper(name), "CONN_") {
				return Config{}, &ConfigError{Message: fmt.Sprintf("%s is invalid at /pair_verification/setup/env: the name %s uses the reserved CONN_ prefix", ConfigPath, name)}
			}
		}
		for _, key := range sortedKeys(loose.Checks) {
			for _, name := range sortedKeys(loose.Checks[key].Env) {
				if strings.HasPrefix(strings.ToUpper(name), "CONN_") {
					return Config{}, &ConfigError{Message: fmt.Sprintf("%s is invalid at /checks/%s/env: the name %s uses the reserved CONN_ prefix", ConfigPath, key, name)}
				}
			}
		}
	}
	document, err := protocol.Decode[protocol.ChecksConfig](data)
	var schemaErr *contracts.ValidationError
	switch {
	case errors.As(err, &schemaErr) && len(schemaErr.Violations) > 0:
		first := schemaErr.Violations[0]
		location := first.InstancePath
		if location == "" {
			location = "the document root"
		}
		return Config{}, &ConfigError{Message: fmt.Sprintf("%s is invalid at %s: %s", ConfigPath, location, first.Message)}
	case err != nil:
		return Config{}, &ConfigError{Message: fmt.Sprintf("%s is not valid JSON: %v", ConfigPath, err)}
	}
	sum := sha256.Sum256(data)
	if pair := document.PairVerification; pair != nil {
		for i, key := range pair.Checks {
			if _, ok := document.Checks[key]; !ok {
				return Config{}, &ConfigError{Message: fmt.Sprintf("%s is invalid at /pair_verification/checks/%d: %q names no check defined in this file", ConfigPath, i, key)}
			}
		}
	}
	return Config{Document: document, SHA256: hex.EncodeToString(sum[:])}, nil
}

// Check returns one check definition.
func (c Config) Check(key string) (protocol.CheckDefinition, error) {
	check, ok := c.Document.Checks[key]
	if !ok {
		return protocol.CheckDefinition{}, &ConfigError{Message: fmt.Sprintf("%s defines no check %q; defined checks: %s", ConfigPath, key, strings.Join(sortedKeys(c.Document.Checks), ", "))}
	}
	return check, nil
}

// CommandSHA256 hashes the canonical JSON of one check definition: its
// fields in schema order and environment names sorted.
func CommandSHA256(check protocol.CheckDefinition) string {
	data, err := json.Marshal(check)
	if err != nil {
		// A CheckDefinition holds only strings, integers, and maps of strings.
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// WorkingDirectory resolves the check's directory inside root, following
// symlinks, and rejects anything outside it.
func WorkingDirectory(root string, check protocol.CheckDefinition) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	relative := check.WorkingDirectory
	if relative == "" {
		relative = "."
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", &ConfigError{Message: "working_directory must be relative to the repository"}
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, filepath.FromSlash(relative)))
	if err != nil {
		return "", &ConfigError{Message: fmt.Sprintf("working_directory %q does not exist in the repository", check.WorkingDirectory)}
	}
	within, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return "", &ConfigError{Message: fmt.Sprintf("working_directory %q resolves outside the repository", check.WorkingDirectory)}
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return "", &ConfigError{Message: fmt.Sprintf("working_directory %q is not a directory", check.WorkingDirectory)}
	}
	return resolved, nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
