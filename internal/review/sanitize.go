package review

import (
	"strings"
	"unicode"
)

// Participant-authored text bounds in the packet.
const (
	maxSummaryRunes   = 4000
	maxStatementRunes = 1000
	maxTitleRunes     = 200
	maxExcerptRunes   = 500
	truncationMarker  = "…"
)

// SanitizeProse removes control characters other than line breaks and tabs,
// escapes angle brackets so no markup survives, trims, and bounds the text to
// limit runes. Multi-line prose keeps its line breaks.
func SanitizeProse(text string, limit int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r), r == '\u2028', r == '\u2029', r == '\ufeff':
			return -1
		}
		return r
	}, text)
	text = strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(text)
	return bound(strings.TrimSpace(text), limit)
}

// SanitizeLine is SanitizeProse for one line: line breaks and tabs become
// spaces.
func SanitizeLine(text string, limit int) string {
	return SanitizeProse(strings.Join(strings.Fields(strings.ReplaceAll(text, "\t", " ")), " "), limit)
}

// SanitizeTitle bounds a display name or title to the title contract.
func SanitizeTitle(text string) string {
	if line := SanitizeLine(text, maxTitleRunes); line != "" {
		return line
	}
	return "untitled"
}

func sanitizeStatement(text string) string { return SanitizeLine(text, maxStatementRunes) }

func bound(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + truncationMarker
}

// boundedStatements sanitizes, deduplicates, and bounds statements. It
// returns the kept statements and the number omitted.
func boundedStatements(values []string, limit int) ([]string, int) {
	result := make([]string, 0, min(len(values), limit))
	seen := map[string]bool{}
	omitted := 0
	for _, value := range values {
		value = sanitizeStatement(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		if len(result) == limit {
			omitted++
			continue
		}
		result = append(result, value)
	}
	return result, omitted
}
