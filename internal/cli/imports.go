package cli

import (
	"fmt"

	"uuid"

	"github.com/Juanqu261/conn/internal/kirospec"
	"github.com/Juanqu261/conn/internal/protocol"
)

// maxImportSourceBytes bounds a source file read solely to calculate its
// provenance digest. Source bytes do not leave this process or enter Conn.
const maxImportSourceBytes = kirospec.MaxSourceBytes

func readKiroSpecHash(path string) (string, error) {
	hash, err := kirospec.Hash(path)
	if err != nil {
		return "", usageError{err}
	}
	return hash, nil
}

func safeSourceReference(value string) (string, error) {
	ref, err := kirospec.SafeReference(value)
	if err != nil {
		return "", usageError{fmt.Errorf("--source-ref: %w", err)}
	}
	return ref, nil
}

func importKiroSpecInput(objectiveID, accountableID uuid.UUID, key, sourceRef, sourceRevisionRef, contentHash, title, overview string) protocol.ObjectiveImportInput {
	return protocol.ObjectiveImportInput{
		ObjectiveID: objectiveID, Key: key, SourceKind: "kiro_spec", SourceRef: sourceRef,
		SourceRevisionRef: sourceRevisionRef, ContentHash: contentHash, Title: title,
		OverviewMarkdown: overview, AccountableHumanID: accountableID,
	}
}
