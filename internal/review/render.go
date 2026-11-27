package review

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Juanqu261/conn/internal/protocol"
)

// MaxMarkdownChars keeps a rendered packet below GitHub's 65,536-character
// pull-request body limit with room for a human preamble.
const MaxMarkdownChars = 60_000

// maxLineRunes bounds each rendered list item.
const maxLineRunes = 600

// PacketFooter states what the packet is and what it never contains.
const PacketFooter = "Generated from Conn records. This packet contains identifiers, subjects, paths, logins, states, and bounded sanitized prose. It contains no source, diffs, prompts, transcripts, credentials, or logs."

// RenderJSON encodes the packet after validating it against its schema.
func RenderJSON(packet protocol.ReviewPacket) (string, error) {
	data, err := protocol.Encode(packet)
	if err != nil {
		return "", err
	}
	var indented strings.Builder
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	encoder := json.NewEncoder(&indented)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return indented.String(), nil
}

// RenderMarkdown renders the packet as pull-request-ready Markdown. Sections
// keep their order. While the output is not under MaxMarkdownChars, the
// longest list is cut from its end, and each truncation is stated.
func RenderMarkdown(packet protocol.ReviewPacket) string {
	working := clonePacket(packet)
	var notes []string
	for {
		text := renderMarkdown(working, notes)
		if utf8.RuneCountInString(text) < MaxMarkdownChars {
			return text
		}
		note, ok := truncateOnce(&working)
		if !ok {
			// Every list is empty; only bounded fixed text remains.
			return boundMarkdown(text)
		}
		notes = append(notes, note)
	}
}

// truncateOnce halves one list and describes what it removed.
func truncateOnce(packet *protocol.ReviewPacket) (string, bool) {
	drop := func(n int) int { return (n + 1) / 2 }
	lists := []struct {
		name string
		size func() int
		cut  func(int)
	}{
		{"known unknowns", func() int { return len(packet.KnownUnknowns) }, func(k int) { packet.KnownUnknowns = packet.KnownUnknowns[:k] }},
		{"open questions", func() int { return len(packet.OpenQuestions) }, func(k int) { packet.OpenQuestions = packet.OpenQuestions[:k] }},
		{"integrity notes", func() int { return len(packet.IntegrityNotes) }, func(k int) { packet.IntegrityNotes = packet.IntegrityNotes[:k] }},
		{"collisions", func() int { return len(packet.Collisions) }, func(k int) { packet.Collisions = packet.Collisions[:k] }},
		{"claims not observable by analysis", func() int { return len(packet.Drift.NotObservable) }, func(k int) { packet.Drift.NotObservable = packet.Drift.NotObservable[:k] }},
		{"changed files without a declared claim", func() int { return len(packet.Drift.ChangedFilesWithoutClaim) }, func(k int) {
			packet.Drift.ChangedFilesWithoutClaim = packet.Drift.ChangedFilesWithoutClaim[:k]
		}},
		{"declared but unobserved subjects", func() int { return len(packet.Drift.DeclaredNotObserved) }, func(k int) { packet.Drift.DeclaredNotObserved = packet.Drift.DeclaredNotObserved[:k] }},
		{"undeclared observed subjects", func() int { return len(packet.Drift.ObservedNotDeclared) }, func(k int) { packet.Drift.ObservedNotDeclared = packet.Drift.ObservedNotDeclared[:k] }},
		{"matched subjects", func() int { return len(packet.Drift.Matched) }, func(k int) { packet.Drift.Matched = packet.Drift.Matched[:k] }},
		{"acceptance criteria", func() int { return len(packet.Acceptance) }, func(k int) { packet.Acceptance = packet.Acceptance[:k] }},
		{"assumptions", func() int { return len(packet.Intent.Assumptions) }, func(k int) { packet.Intent.Assumptions = packet.Intent.Assumptions[:k] }},
	}
	// Halve the longest list, preferring later sections on ties, so short
	// lists such as integrity notes survive a long tail of files.
	chosen := -1
	for i, list := range lists {
		if n := list.size(); n > 0 && (chosen < 0 || n > lists[chosen].size()) {
			chosen = i
		}
	}
	if chosen < 0 {
		return "", false
	}
	n := lists[chosen].size()
	removed := drop(n)
	lists[chosen].cut(n - removed)
	return fmt.Sprintf("Markdown omitted the last %d %s to stay under %d characters; the JSON packet has them.", removed, lists[chosen].name, MaxMarkdownChars), true
}

func renderMarkdown(packet protocol.ReviewPacket, renderNotes []string) string {
	var out strings.Builder
	line := func(format string, values ...any) { fmt.Fprintf(&out, format+"\n", values...) }
	item := func(format string, values ...any) {
		out.WriteString("- " + bound(fmt.Sprintf(format, values...), maxLineRunes) + "\n")
	}
	intent := packet.Intent

	line("# Conn review: Intent `%s` revision %d for %s", shortID(intent.ID), intent.Revision, markdownText(packet.Objective.Title))
	line("")
	status := "active"
	if !intent.Active {
		status = "not active"
	}
	line("- Intent: `%s` revision %d by @%s (participant `%s`), %s", intent.ID, intent.Revision, intent.OwnerLogin, intent.ParticipantID, status)
	line("- Objective: `%s` revision %d, %s", packet.Objective.ID, packet.Objective.Revision, markdownText(packet.Objective.Title))
	line("- Project: %s (`%s`)", markdownText(packet.Project.Name), packet.Project.ID)
	if packet.Blueprint != nil {
		line("- Blueprint: `%s` revision %d", packet.Blueprint.ID, packet.Blueprint.Revision)
	}
	if packet.Charter != nil {
		accepted := "not accepted"
		if packet.Charter.Accepted {
			accepted = "accepted"
		}
		line("- Charter: `%s` (%s)", packet.Charter.ID, accepted)
	}
	git := packet.Git
	head := code(shortCommit(git.Head))
	if git.HeadIsWorktree {
		head += " plus uncommitted changes"
	}
	branch := "unknown"
	if git.Branch != "" {
		branch = code(git.Branch)
	}
	line("- Git: branch %s, base %s, head %s, %d changed file(s)", branch, code(shortCommit(git.Base)), head, git.ChangedFileCount)
	if git.ChecksConfigChanged {
		line("- **`%s` changed on this branch.**", ChecksConfigPath)
	}

	line("")
	line("## What this change set out to do")
	line("")
	for _, text := range strings.Split(markdownProse(intent.Summary), "\n") {
		line("> %s", text)
	}
	line("")
	if len(intent.Assumptions) == 0 {
		line("No assumptions were declared.")
	} else {
		line("Assumptions:")
		line("")
		for _, assumption := range intent.Assumptions {
			item("%s", markdownText(assumption))
		}
	}

	line("")
	line("## Acceptance evidence")
	line("")
	if len(packet.Acceptance) == 0 {
		line("The Objective revision has no keyed acceptance criteria.")
	} else {
		line("| Key | State | Provenance | Commit | Stale |")
		line("| --- | --- | --- | --- | --- |")
		for _, acceptance := range packet.Acceptance {
			provenance, commit, stale := "none", "", "n/a"
			if len(acceptance.Evidence) > 0 {
				latest := acceptance.Evidence[0]
				provenance = provenanceLabel(latest.Provenance)
				if latest.Commit != "" {
					commit = code(shortCommit(latest.Commit))
					stale = map[bool]string{true: "**stale**", false: "no"}[latest.Stale]
				}
				if latest.Dirty {
					provenance += ", uncommitted changes"
				}
			}
			line("| %s | %s | %s | %s | %s |", code(acceptance.Key), acceptance.State, provenance, commit, stale)
		}
		line("")
		for _, acceptance := range packet.Acceptance {
			item("%s: %s", code(acceptance.Key), markdownText(acceptance.Statement))
		}
	}

	line("")
	line("## Scope drift")
	line("")
	drift := packet.Drift
	switch drift.Analysis.Status {
	case protocol.AnalysisNotAnalyzed:
		if drift.Analysis.IntentRevision != nil {
			line("Not analyzed: observed analysis covers revision %d, not revision %d. Drift is not reported, which is not evidence of no drift.", *drift.Analysis.IntentRevision, intent.Revision)
		} else {
			line("Not analyzed. Drift is not reported, which is not evidence of no drift.")
		}
	case protocol.AnalysisIncomplete:
		line("Analysis of revision %d is **incomplete** (%s). Drift may miss changes.", intent.Revision, strings.Join(drift.Analysis.DiagnosticCodes, ", "))
	default:
		line("Analysis of revision %d is complete.", intent.Revision)
	}
	subjects := func(title string, items []protocol.DriftSubject, omitted int) {
		if len(items) == 0 && omitted == 0 {
			return
		}
		line("")
		line("**%s** (%d)", title, len(items)+omitted)
		line("")
		for _, subject := range items {
			item("%s %s (%s)", subject.Relation, code(subject.NormalizedSubject), markdownText(subject.DisplayName))
		}
	}
	if drift.Analysis.Status != protocol.AnalysisNotAnalyzed {
		subjects("Changed without a declared claim", drift.ObservedNotDeclared, drift.Omitted.ObservedNotDeclared)
		subjects("Declared but not observed", drift.DeclaredNotObserved, drift.Omitted.DeclaredNotObserved)
		subjects("Matched", drift.Matched, drift.Omitted.Matched)
		if len(drift.Matched)+len(drift.ObservedNotDeclared)+len(drift.DeclaredNotObserved) == 0 {
			line("")
			line("No declared or observed code-symbol or contract change was compared.")
		}
	}
	if len(drift.ChangedFilesWithoutClaim) > 0 {
		line("")
		line("**Changed files without a declared claim** (%d)", len(drift.ChangedFilesWithoutClaim)+drift.Omitted.ChangedFilesWithoutClaim)
		line("")
		for _, file := range drift.ChangedFilesWithoutClaim {
			written := ""
			if file.WrittenByAgent {
				written = " (written by a Conn-connected agent session)"
			}
			item("%s%s", code(file.Path), written)
		}
	}
	if len(drift.NotObservable) > 0 {
		line("")
		line("**Not observable by analysis** (%d)", len(drift.NotObservable)+drift.Omitted.NotObservable)
		line("")
		for _, claim := range drift.NotObservable {
			evidence := ""
			if claim.AcceptanceKey != "" {
				evidence = fmt.Sprintf("; acceptance %s is %s", code(claim.AcceptanceKey), claim.EvidenceState)
			}
			item("%s %s (%s)%s", claim.Relation, code(claim.NormalizedSubject), markdownText(claim.DisplayName), evidence)
		}
	}

	line("")
	line("## Collisions and decisions")
	line("")
	if len(packet.Collisions) == 0 {
		line("No collision involves this Intent revision.")
	}
	for _, collision := range packet.Collisions {
		text := fmt.Sprintf("%s %s, %s, **%s**, with Intent %s revision %d", code(shortID(collision.ID)), collision.RuleID, collision.Severity,
			strings.ReplaceAll(collision.State, "_", " "), code(shortID(collision.Counterpart.IntentID)), collision.Counterpart.Revision)
		if len(collision.MatchedSubjects) > 0 {
			matched := make([]string, 0, len(collision.MatchedSubjects))
			for _, subject := range collision.MatchedSubjects {
				matched = append(matched, code(subject))
			}
			text += "; matched " + strings.Join(matched, ", ")
		}
		if decision := collision.Decision; decision != nil {
			binding := "non-binding"
			if decision.Binding {
				binding = "binding"
			}
			by := ""
			if decision.DecidedByLogin != "" {
				by = " by @" + decision.DecidedByLogin
			}
			text += fmt.Sprintf(". Decision: %s (%s)%s: \"%s\"", decision.Kind, binding, by, markdownText(decision.RationaleExcerpt))
		}
		if collision.ProposalCount != nil {
			text += fmt.Sprintf(". Proposals: %d", *collision.ProposalCount)
		}
		if collision.LatestCombinationOutcome != "" {
			text += fmt.Sprintf(". Latest combination check: **%s** (run on a participant's machine)", strings.ReplaceAll(collision.LatestCombinationOutcome, "_", " "))
		}
		item("%s", text)
	}

	statements := func(title, empty string, values []string) {
		line("")
		line("## %s", title)
		line("")
		if len(values) == 0 {
			line("%s", empty)
		}
		for _, value := range values {
			item("%s", markdownText(value))
		}
	}
	statements("Integrity notes", "None recorded.", packet.IntegrityNotes)
	statements("Open questions", "None recorded.", packet.OpenQuestions)
	statements("Known unknowns", "None recorded. Absence does not prove completion.", packet.KnownUnknowns)
	if truncated := append(append([]string{}, packet.Truncated...), renderNotes...); len(truncated) > 0 {
		statements("Truncated", "", truncated)
	}
	line("")
	line("---")
	line("")
	line("Conn review packet version %d at Project event %d. %s", packet.PacketVersion, packet.GeneratedAtEvent, PacketFooter)
	return out.String()
}

func provenanceLabel(provenance string) string {
	switch provenance {
	case protocol.ProvenanceObservedRun:
		return "observed run (owner's machine)"
	case protocol.ProvenanceReported:
		return "reported"
	}
	return "none"
}

// markdownText neutralizes Markdown that could hide or restyle content in a
// one-line context: table pipes, images, and raw line breaks.
func markdownText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return strings.NewReplacer("|", "\\|", "![", "!\\[", "`", "'").Replace(text)
}

// markdownProse keeps line breaks for a block quote and neutralizes images.
func markdownProse(text string) string {
	return strings.NewReplacer("![", "!\\[", "\t", " ").Replace(text)
}

func code(text string) string {
	if text == "" {
		return "unknown"
	}
	return "`" + strings.ReplaceAll(strings.ReplaceAll(text, "`", "'"), "|", "\\|") + "`"
}

func boundMarkdown(text string) string {
	runes := []rune(text)
	if len(runes) < MaxMarkdownChars {
		return text
	}
	return string(runes[:MaxMarkdownChars-200]) + "\n\n[Conn truncated this packet at its size limit.]\n"
}

func clonePacket(packet protocol.ReviewPacket) protocol.ReviewPacket {
	clone := packet
	clone.Intent.Assumptions = append([]string(nil), packet.Intent.Assumptions...)
	clone.Acceptance = append([]protocol.ReviewAcceptance(nil), packet.Acceptance...)
	clone.Collisions = append([]protocol.ReviewCollision(nil), packet.Collisions...)
	clone.IntegrityNotes = append([]string(nil), packet.IntegrityNotes...)
	clone.OpenQuestions = append([]string(nil), packet.OpenQuestions...)
	clone.KnownUnknowns = append([]string(nil), packet.KnownUnknowns...)
	clone.Drift.Matched = append([]protocol.DriftSubject(nil), packet.Drift.Matched...)
	clone.Drift.DeclaredNotObserved = append([]protocol.DriftSubject(nil), packet.Drift.DeclaredNotObserved...)
	clone.Drift.ObservedNotDeclared = append([]protocol.DriftSubject(nil), packet.Drift.ObservedNotDeclared...)
	clone.Drift.ChangedFilesWithoutClaim = append([]protocol.DriftFile(nil), packet.Drift.ChangedFilesWithoutClaim...)
	clone.Drift.NotObservable = append([]protocol.DriftNotObservable(nil), packet.Drift.NotObservable...)
	return clone
}
