// Package workstart brings an Objective into Conn from a Kiro spec, a Spec
// Kit feature, a GitHub issue, or an Objective ID, and finds earlier imports of the same source so
// `conn start` reuses an unchanged Objective and offers a revision for a
// changed one. It reads local files only to propose content a human
// confirms, and it never persists anything itself.
package workstart

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"uuid"

	"github.com/Juanqu261/conn/internal/githubissue"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/kirospec"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/speckit"
)

// maxScannedObjectives bounds the search for an earlier import.
const (
	maxScannedObjectives = 200
	maxOverviewRunes     = 2000
)

// Decision is what start does with the source's Objective.
type Decision string

const (
	DecisionExisting Decision = "existing" // an Objective ID was given
	DecisionReuse    Decision = "reuse"    // same source and content hash
	DecisionRevise   Decision = "revise"   // same source, changed content
	DecisionImport   Decision = "import"   // a new Objective
)

// Resolution is the proposed Objective for a source.
type Resolution struct {
	Decision Decision
	// Objective is the stored revision for existing, reuse, and revise.
	Objective *protocol.Objective
	// Input is the import content for revise and import.
	Input protocol.ObjectiveImportInput
	// ExpectedRevision is the current revision a revision appends to.
	ExpectedRevision int
	SourceLabel      string
	Notes            []string
}

// FindImported returns the current Objective revision imported from the
// same source kind and reference, or nil.
func FindImported(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, kind, ref string) (*protocol.Objective, error) {
	status, err := client.Status(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i, summary := range status.Data.Objectives {
		if i == maxScannedObjectives {
			break
		}
		objective, err := client.ObjectiveRevision(ctx, projectID, summary.ObjectiveID, summary.CurrentRevision)
		if err != nil {
			return nil, err
		}
		if provenance := objective.Data.ImportProvenance; provenance != nil && provenance.SourceKind == kind && provenance.SourceRef == ref {
			found := objective.Data
			return &found, nil
		}
	}
	return nil, nil
}

// Resolve compares import content with any earlier import of the same
// source and proposes reuse, a new revision, or a new Objective. The input
// must name its source kind, reference, and content hash.
func Resolve(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, input protocol.ObjectiveImportInput, label string) (Resolution, error) {
	existing, err := FindImported(ctx, client, projectID, input.SourceKind, input.SourceRef)
	if err != nil {
		return Resolution{}, err
	}
	resolution := Resolution{Input: input, SourceLabel: label}
	switch {
	case existing == nil:
		resolution.Decision = DecisionImport
		resolution.Input.ObjectiveID = uuid.NewV7()
	case existing.ImportProvenance.ContentHash == input.ContentHash:
		resolution.Decision, resolution.Objective = DecisionReuse, existing
	default:
		resolution.Decision, resolution.Objective, resolution.ExpectedRevision = DecisionRevise, existing, existing.Revision
		resolution.Input.ObjectiveID = existing.ObjectiveID
		if resolution.Input.Key == "" {
			resolution.Input.Key = existing.Key
		}
	}
	return resolution, nil
}

// ReadObjective reads an Objective's current revision by ID.
func ReadObjective(ctx context.Context, client *httpclient.Client, projectID, objectiveID uuid.UUID) (protocol.Objective, error) {
	status, err := client.Status(ctx, projectID)
	if err != nil {
		return protocol.Objective{}, err
	}
	for _, summary := range status.Data.Objectives {
		if summary.ObjectiveID == objectiveID {
			objective, err := client.ObjectiveRevision(ctx, projectID, objectiveID, summary.CurrentRevision)
			return objective.Data, err
		}
	}
	return protocol.Objective{}, fmt.Errorf("Objective %s does not exist in this Project", objectiveID)
}

// KiroSpecInput proposes import content for a Kiro spec file or directory
// inside root. A directory uses requirements.md, then design.md. The title
// comes from the first heading, the overview from the first paragraph, and
// acceptance criteria from the first acceptance-criteria list.
func KiroSpecInput(root, path string) (protocol.ObjectiveImportInput, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return protocol.ObjectiveImportInput{}, err
	}
	// The root is symlink-resolved, so the spec path must be too.
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return protocol.ObjectiveImportInput{}, fmt.Errorf("read the Kiro spec: %w", err)
	}
	specName := filepath.Base(absolute)
	if info.IsDir() {
		chosen := ""
		for _, name := range []string{"requirements.md", "design.md"} {
			if candidate := filepath.Join(absolute, name); fileExists(candidate) {
				chosen = candidate
				break
			}
		}
		if chosen == "" {
			return protocol.ObjectiveImportInput{}, errors.New("the Kiro spec directory has no requirements.md or design.md")
		}
		absolute = chosen
	} else {
		specName = filepath.Base(filepath.Dir(absolute))
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return protocol.ObjectiveImportInput{}, err
	}
	ref, err := kirospec.SafeReference(relative)
	if err != nil {
		return protocol.ObjectiveImportInput{}, fmt.Errorf("the Kiro spec must be inside the repository: %w", err)
	}
	hash, heading, err := kirospec.HashAndTitle(absolute)
	if err != nil {
		return protocol.ObjectiveImportInput{}, err
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return protocol.ObjectiveImportInput{}, err
	}
	title := heading
	switch strings.ToLower(title) {
	case "", "requirements", "design", "tasks":
		title = humanize(specName)
	}
	overview := firstParagraph(content)
	if overview == "" {
		overview = "Imported from the Kiro spec " + ref + "."
	}
	return protocol.ObjectiveImportInput{
		SourceKind: "kiro_spec", SourceRef: ref, ContentHash: hash, Title: title, OverviewMarkdown: overview,
		KeyPoints: githubissue.SuggestCriteria(string(content)),
	}, nil
}

// SpecKitInput proposes import content for a Spec Kit feature directory, or
// its spec.md, inside root. The source reference is the repository-relative
// spec.md and the content hash its SHA-256 digest. Key points come from the
// FR and SC lines, clarification markers, and constitution principles; notes
// report the constitution version and any key point the 50-point limit
// omitted.
func SpecKitInput(root, path string) (protocol.ObjectiveImportInput, speckit.Feature, []string, error) {
	feature, err := speckit.Resolve(root, path)
	if err != nil {
		return protocol.ObjectiveImportInput{}, speckit.Feature{}, nil, err
	}
	spec, err := speckit.ReadFile(feature.SpecPath())
	if err != nil {
		return protocol.ObjectiveImportInput{}, feature, nil, fmt.Errorf("read the Spec Kit feature: %w", err)
	}
	constitution, err := speckit.FindConstitution(feature.Root)
	if err != nil {
		return protocol.ObjectiveImportInput{}, feature, nil, fmt.Errorf("read the Spec Kit constitution: %w", err)
	}
	overview := speckit.Overview(spec, feature.SpecRef)
	var notes []string
	var principles []byte
	if constitution != nil {
		principles = constitution.Content
		if version := speckit.ConstitutionVersion(constitution.Content); version != "" {
			overview = bound(overview + "\n\nConstitution version " + version + ".")
		}
	}
	points, omitted := speckit.KeyPoints(spec, principles)
	if len(omitted) > 0 {
		keys := make([]string, 0, len(omitted))
		for _, point := range omitted {
			keys = append(keys, point.Key)
		}
		notes = append(notes, fmt.Sprintf("The Objective holds at most %d key points; %d were omitted: %s.", speckit.MaxKeyPoints, len(omitted), strings.Join(keys, ", ")))
	}
	return protocol.ObjectiveImportInput{
		SourceKind: speckit.SourceKind, SourceRef: feature.SpecRef, ContentHash: speckit.Digest(spec),
		Title: speckit.Title(spec, feature.Name()), OverviewMarkdown: overview, KeyPoints: points,
	}, feature, notes, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func humanize(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	if len(words) == 0 {
		return "Kiro spec"
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}

// firstParagraph returns the first non-heading paragraph, bounded.
func firstParagraph(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "#") || line == "---":
			if len(lines) > 0 {
				return bound(strings.Join(lines, " "))
			}
		case line == "":
			if len(lines) > 0 {
				return bound(strings.Join(lines, " "))
			}
		default:
			lines = append(lines, line)
		}
	}
	return bound(strings.Join(lines, " "))
}

func bound(text string) string {
	if utf8.RuneCountInString(text) <= maxOverviewRunes {
		return text
	}
	return string([]rune(text)[:maxOverviewRunes-1]) + "…"
}
