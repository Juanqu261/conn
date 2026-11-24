package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// maxImportSourceBytes bounds a source file read solely to calculate its
// provenance digest. Source bytes do not leave this process or enter Conn.
const maxImportSourceBytes int64 = 1 << 20

func readKiroSpecHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", usageError{fmt.Errorf("read Kiro specification: %w", err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", usageError{fmt.Errorf("Kiro specification source must be a regular non-symlink file")}
	}
	if info.Size() > maxImportSourceBytes {
		return "", usageError{fmt.Errorf("Kiro specification source is larger than 1 MiB")}
	}
	file, err := os.Open(path)
	if err != nil {
		return "", usageError{fmt.Errorf("read Kiro specification: %w", err)}
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() > maxImportSourceBytes {
		return "", usageError{fmt.Errorf("Kiro specification source must remain a regular file no larger than 1 MiB")}
	}
	hash := sha256.New()
	copied, err := io.Copy(hash, io.LimitReader(file, maxImportSourceBytes+1))
	if err != nil {
		return "", usageError{fmt.Errorf("read Kiro specification: %w", err)}
	}
	if copied > maxImportSourceBytes {
		return "", usageError{fmt.Errorf("Kiro specification source is larger than 1 MiB")}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func safeSourceReference(value string) (string, error) {
	normalized := strings.ReplaceAll(value, `\`, "/")
	if normalized == "" || filepath.IsAbs(value) || strings.HasPrefix(normalized, "/") {
		return "", usageError{fmt.Errorf("--source-ref must be a relative reference")}
	}
	clean := filepath.ToSlash(filepath.Clean(normalized))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", usageError{fmt.Errorf("--source-ref must not traverse outside its local specification")}
	}
	return clean, nil
}

func importKiroSpecInput(objectiveID, accountableID uuid.UUID, key, sourceRef, sourceRevisionRef, contentHash, title, overview string) protocol.ObjectiveImportInput {
	return protocol.ObjectiveImportInput{
		ObjectiveID: objectiveID, Key: key, SourceKind: "kiro_spec", SourceRef: sourceRef,
		SourceRevisionRef: sourceRevisionRef, ContentHash: contentHash, Title: title,
		OverviewMarkdown: overview, AccountableHumanID: accountableID,
	}
}
