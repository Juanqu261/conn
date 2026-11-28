// Package speckit reads a local GitHub Spec Kit feature to propose the
// Objective content and the planned paths a human confirms. Extraction is
// deterministic: fixed rules, not an agent, decide which requirements and
// paths a feature names. Only confirmed Objective content and path-derived
// claims ever reach Conn; task, plan, research, contract, and data-model text
// stay on this machine. The package never executes text from a Spec Kit file
// and never writes into .specify/ or any other Spec Kit file.
package speckit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Juanqu261/conn/internal/kirospec"
	"github.com/Juanqu261/conn/internal/protocol"
)

// Bounds.
const (
	SourceKind = protocol.SourceKindSpecKitFeature
	// MaxFileBytes bounds every Spec Kit file Conn reads.
	MaxFileBytes = kirospec.MaxSourceBytes
	// MaxPaths bounds the planned paths of one feature.
	MaxPaths = 200
	// MaxKeyPoints is the Objective key-point limit.
	MaxKeyPoints = 50

	maxStatementRunes = 1000
	maxTitleRunes     = 200
	maxOverviewRunes  = 2000
	maxKeyLength      = 64
	maxTaskIDs        = 1000
)

// File names inside a feature directory.
const (
	SpecFile  = "spec.md"
	TasksFile = "tasks.md"
	PlanFile  = "plan.md"
)

// ConstitutionPaths are the repository-relative constitution locations in
// lookup order. Spec Kit 1.4 writes .specify/memory/constitution.md; its
// methodology document names memory/constitution.md.
var ConstitutionPaths = []string{".specify/memory/constitution.md", "memory/constitution.md"}

// FeatureFile is the file in which Spec Kit records the current feature.
const FeatureFile = ".specify/feature.json"

// FeatureDirectoryEnv overrides the current feature, as in Spec Kit's scripts.
const FeatureDirectoryEnv = "SPECIFY_FEATURE_DIRECTORY"

// ErrNotFeature means a path is not a Spec Kit feature.
var ErrNotFeature = errors.New("a Spec Kit feature is a directory that contains spec.md, such as specs/001-feature-name, or that spec.md file")

// ReadFile reads a regular, non-symlink file no larger than MaxFileBytes.
func ReadFile(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file, not a symbolic link", filepath.Base(name))
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%s is larger than 1 MiB", filepath.Base(name))
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%s changed while it was read", filepath.Base(name))
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxFileBytes {
		return nil, fmt.Errorf("%s is larger than 1 MiB", filepath.Base(name))
	}
	return content, nil
}

// Feature is one Spec Kit feature directory inside a repository.
type Feature struct {
	// Root is the symlink-resolved repository root.
	Root string
	// Dir is the absolute feature directory.
	Dir string
	// DirRef and SpecRef are repository-relative, with forward slashes.
	DirRef  string
	SpecRef string
}

// Name is the feature directory name, such as 003-chat-system.
func (f Feature) Name() string { return filepath.Base(f.Dir) }

// SpecPath is the absolute path of spec.md.
func (f Feature) SpecPath() string { return filepath.Join(f.Dir, SpecFile) }

// IsFeaturePath reports whether path is a directory containing spec.md or a
// file named spec.md. It does not check that the path is in a repository.
func IsFeaturePath(name string) bool {
	info, err := os.Stat(name)
	if err != nil {
		return false
	}
	if info.IsDir() {
		spec, err := os.Lstat(filepath.Join(name, SpecFile))
		return err == nil && spec.Mode().IsRegular()
	}
	return filepath.Base(name) == SpecFile && info.Mode().IsRegular()
}

// Resolve finds the feature for a directory or spec.md path inside root.
func Resolve(root, name string) (Feature, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Feature{}, fmt.Errorf("resolve the repository: %w", err)
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return Feature{}, err
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Feature{}, fmt.Errorf("read the Spec Kit feature: %w", err)
	}
	dir := absolute
	if !info.IsDir() {
		if filepath.Base(absolute) != SpecFile {
			return Feature{}, ErrNotFeature
		}
		dir = filepath.Dir(absolute)
	}
	if spec, err := os.Lstat(filepath.Join(dir, SpecFile)); err != nil || !spec.Mode().IsRegular() {
		return Feature{}, ErrNotFeature
	}
	relative, err := filepath.Rel(resolvedRoot, filepath.Join(dir, SpecFile))
	if err != nil {
		return Feature{}, fmt.Errorf("the Spec Kit feature must be inside the repository: %w", err)
	}
	specRef, err := kirospec.SafeReference(relative)
	if err != nil {
		return Feature{}, fmt.Errorf("the Spec Kit feature must be inside the repository: %w", err)
	}
	return Feature{Root: resolvedRoot, Dir: dir, DirRef: path.Dir(specRef), SpecRef: specRef}, nil
}

// Digest returns the lowercase hexadecimal SHA-256 digest of content.
func Digest(content []byte) string { return fmt.Sprintf("%x", sha256.Sum256(content)) }

// Constitution is the repository's Spec Kit constitution, when it has one.
type Constitution struct {
	Ref     string
	Content []byte
}

// FindConstitution returns the first regular constitution file in
// ConstitutionPaths. A missing constitution is not an error.
func FindConstitution(root string) (*Constitution, error) {
	for _, ref := range ConstitutionPaths {
		name := filepath.Join(root, filepath.FromSlash(ref))
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		content, err := ReadFile(name)
		if err != nil {
			return nil, err
		}
		return &Constitution{Ref: ref, Content: content}, nil
	}
	return nil, nil
}

var (
	requirementLine   = regexp.MustCompile(`^\s*[-*]\s*\*\*FR-(\d+)\*\*:\s*(.+?)\s*$`)
	successLine       = regexp.MustCompile(`^\s*[-*]\s*\*\*SC-(\d+)\*\*:\s*(.+?)\s*$`)
	clarification     = regexp.MustCompile(`\[NEEDS CLARIFICATION:\s*([^\]]+)\]`)
	placeholderOnly   = regexp.MustCompile(`^\[[^\]]*\]$`)
	principleHeading  = regexp.MustCompile(`^\s{0,3}###\s+(.+?)\s*#*\s*$`)
	anyHeading        = regexp.MustCompile(`^\s{0,3}#{1,6}\s`)
	numeralPrefix     = regexp.MustCompile(`^(?:[IVXLCDM]+|[0-9]+)[.)]\s+`)
	parenthetical     = regexp.MustCompile(`\([^)]*\)`)
	versionLine       = regexp.MustCompile(`\*\*Version\*\*:\s*([^\s|*]+)`)
	metadataLine      = regexp.MustCompile(`^\*\*[^*]+\*\*:`)
	listItem          = regexp.MustCompile(`^(?:[-*+]|[0-9]{1,3}[.)])\s`)
	inputLine         = regexp.MustCompile(`^\*\*Input\*\*:\s*(?:User description:\s*)?(.+)$`)
	numberedDirectory = regexp.MustCompile(`^[0-9]+(?:-[0-9]+)*-`)
	htmlComment       = regexp.MustCompile(`<!--.*?-->`)
)

// Omission names key points left out by the Objective limit.
type Omission struct {
	Kind protocol.KeyPointKind
	Key  string
}

// KeyPoints suggests Objective key points: one acceptance_criterion per
// **FR-NNN** requirement keyed fr-nnn, one outcome per **SC-NNN** success
// criterion keyed sc-nnn, one question per [NEEDS CLARIFICATION: ...]
// marker, and one constraint per constitution principle heading. At most
// MaxKeyPoints are kept in that priority order; the rest are returned as
// omitted. constitution may be nil.
func KeyPoints(spec, constitution []byte) ([]protocol.KeyPoint, []Omission) {
	var all []protocol.KeyPoint
	used := map[string]bool{}
	add := func(kind protocol.KeyPointKind, key, statement string) {
		statement = statementText(statement)
		if statement == "" || placeholderOnly.MatchString(statement) || used[key] {
			return
		}
		used[key] = true
		all = append(all, protocol.KeyPoint{Key: key, Kind: kind, Statement: statement})
	}
	lines := splitLines(spec)
	for _, line := range lines {
		if match := requirementLine.FindStringSubmatch(line); match != nil {
			add(protocol.KeyPointAcceptanceCriterion, "fr-"+match[1], match[2])
		}
	}
	for _, line := range lines {
		if match := successLine.FindStringSubmatch(line); match != nil {
			add(protocol.KeyPointOutcome, "sc-"+match[1], match[2])
		}
	}
	questions := map[string]bool{}
	for _, match := range clarification.FindAllStringSubmatch(string(spec), -1) {
		text := statementText(match[1])
		if text == "" || questions[text] {
			continue
		}
		questions[text] = true
		add(protocol.KeyPointQuestion, fmt.Sprintf("question-%d", len(questions)), text)
	}
	for _, principle := range principles(constitution) {
		add(protocol.KeyPointConstraint, uniqueKey(principle.key, used), principle.statement)
	}
	if len(all) <= MaxKeyPoints {
		return all, nil
	}
	omitted := make([]Omission, 0, len(all)-MaxKeyPoints)
	for _, point := range all[MaxKeyPoints:] {
		omitted = append(omitted, Omission{Kind: point.Kind, Key: point.Key})
	}
	return all[:MaxKeyPoints], omitted
}

type principle struct{ key, statement string }

// principles returns the constitution's ### headings that are not template
// placeholders, each with the first sentence that follows it.
func principles(constitution []byte) []principle {
	if len(constitution) == 0 {
		return nil
	}
	lines := splitLines(constitution)
	var found []principle
	for i, line := range lines {
		match := principleHeading.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name := strings.TrimSpace(htmlComment.ReplaceAllString(match[1], ""))
		if name == "" || placeholderOnly.MatchString(name) {
			continue
		}
		keyName := strings.TrimSpace(parenthetical.ReplaceAllString(numeralPrefix.ReplaceAllString(name, ""), ""))
		slugged := slug(keyName)
		if slugged == "" {
			continue
		}
		key := "constitution-" + slugged
		if len(key) > maxKeyLength {
			key = strings.TrimRight(key[:maxKeyLength], "-")
		}
		statement := name
		if sentence := firstSentence(lines[i+1:]); sentence != "" {
			statement = strings.TrimRight(name, ".") + ". " + sentence
		}
		found = append(found, principle{key: key, statement: statement})
	}
	return found
}

// firstSentence returns the first sentence of the text before the next
// heading, skipping comments, blank lines, and placeholders.
func firstSentence(lines []string) string {
	var text []string
	comment := false
	for _, line := range lines {
		if anyHeading.MatchString(line) {
			break
		}
		trimmed := strings.TrimSpace(htmlComment.ReplaceAllString(line, ""))
		if comment {
			if strings.Contains(line, "-->") {
				comment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			comment = !strings.Contains(trimmed, "-->")
			continue
		}
		if trimmed == "" {
			if len(text) > 0 {
				break
			}
			continue
		}
		if placeholderOnly.MatchString(trimmed) {
			return ""
		}
		text = append(text, strings.TrimLeft(trimmed, "-*+ "))
	}
	joined := strings.Join(text, " ")
	for i, r := range joined {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(joined) || joined[i+1] == ' ') {
			return joined[:i+1]
		}
	}
	return joined
}

// ConstitutionVersion returns the constitution's **Version**: value, or ""
// when it is absent or a placeholder.
func ConstitutionVersion(constitution []byte) string {
	match := versionLine.FindSubmatch(constitution)
	if match == nil || bytes.HasPrefix(match[1], []byte("[")) {
		return ""
	}
	return string(match[1])
}

// Title suggests the Objective title: the first level-one heading without a
// leading "Feature Specification:", or the humanized directory name without
// its number prefix when the heading is absent or a placeholder.
func Title(spec []byte, directoryName string) string {
	title := ""
	for _, line := range splitLines(spec) {
		if heading, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			title = strings.TrimSpace(heading)
			break
		}
	}
	if prefix := "feature specification:"; len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
		title = strings.TrimSpace(title[len(prefix):])
	}
	title = strings.Join(strings.Fields(strings.ReplaceAll(title, "\x00", "")), " ")
	if title == "" || placeholderOnly.MatchString(title) || strings.Contains(title, "[FEATURE NAME]") {
		title = humanize(numberedDirectory.ReplaceAllString(directoryName, ""))
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		title = string([]rune(title)[:maxTitleRunes-1]) + "…"
	}
	return title
}

func humanize(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	if len(words) == 0 {
		return "Spec Kit feature"
	}
	first := []rune(words[0])
	words[0] = string(unicode.ToUpper(first[0])) + string(first[1:])
	return strings.Join(words, " ")
}

// Overview suggests the Objective overview: the first paragraph under the
// title that is not a **Field**: metadata line, then the **Input** feature
// description, then a sentence naming the feature.
func Overview(spec []byte, specRef string) string {
	lines := splitLines(spec)
	start := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			start = i + 1
			break
		}
	}
	var paragraph []string
	input := ""
	comment := false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if anyHeading.MatchString(line) {
			break
		}
		if comment {
			comment = !strings.Contains(trimmed, "-->")
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			comment = !strings.Contains(trimmed, "-->")
			continue
		}
		switch {
		case trimmed == "" || trimmed == "---":
			if len(paragraph) > 0 {
				return bound(strings.Join(paragraph, " "), maxOverviewRunes)
			}
		case metadataLine.MatchString(trimmed):
			if match := inputLine.FindStringSubmatch(trimmed); match != nil && input == "" {
				input = strings.Trim(strings.TrimSpace(match[1]), `"`)
			}
		case listItem.MatchString(trimmed):
			// Requirements and scenarios are key points, not the overview.
			if len(paragraph) > 0 {
				return bound(strings.Join(paragraph, " "), maxOverviewRunes)
			}
		default:
			paragraph = append(paragraph, trimmed)
		}
	}
	if len(paragraph) > 0 {
		return bound(strings.Join(paragraph, " "), maxOverviewRunes)
	}
	if input != "" && !strings.Contains(input, "$ARGUMENTS") {
		return bound(input, maxOverviewRunes)
	}
	return "Imported from the Spec Kit feature `" + specRef + "`."
}

// Extraction is the deterministic list of a feature's planned paths.
type Extraction struct {
	// Source is protocol.SpecKitSourceTasks, SpecKitSourcePlan, or
	// SpecKitSourceNone.
	Source string
	// Paths are accepted repository-relative file paths in first-appearance
	// order, deduplicated, at most MaxPaths.
	Paths []string
	// NewPaths are the accepted paths that do not exist yet.
	NewPaths []string
	// Unresolved are path-shaped tokens whose parent directory does not exist.
	Unresolved []string
	// TasksWithoutPath are the IDs of task lines that named no accepted path.
	TasksWithoutPath []string
	Truncated        bool
}

var (
	taskLine     = regexp.MustCompile(`^\s*[-*]\s*\[[ xX]\]\s*(T[0-9]+)\b(.*)$`)
	taskMarker   = regexp.MustCompile(`\[(?:P|US[0-9]+)\]`)
	backtickSpan = regexp.MustCompile("`([^`]+)`")
	extension    = regexp.MustCompile(`\.[A-Za-z0-9]{1,8}$`)
	fence        = regexp.MustCompile("^\\s*(```|~~~)")
	treeChars    = "│├└─┬┼┤┌┐┘┴|`+- \t "
)

// PlannedPaths extracts the feature's planned paths from tasks.md, or, when
// it is absent, from the source-structure code block of plan.md. Only
// os.Lstat and bounded reads touch the file system.
func PlannedPaths(feature Feature) (Extraction, error) {
	extraction := Extraction{Source: protocol.SpecKitSourceNone, Paths: []string{}, NewPaths: []string{}, Unresolved: []string{}, TasksWithoutPath: []string{}}
	collector := newCollector(feature.Root, &extraction)
	tasks := filepath.Join(feature.Dir, TasksFile)
	if _, err := os.Lstat(tasks); err == nil {
		content, err := ReadFile(tasks)
		if err != nil {
			return extraction, err
		}
		extraction.Source = protocol.SpecKitSourceTasks
		for _, line := range splitLines(content) {
			match := taskLine.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			if accepted := collector.addAll(taskTokens(match[2])); accepted == 0 && len(extraction.TasksWithoutPath) < maxTaskIDs {
				extraction.TasksWithoutPath = append(extraction.TasksWithoutPath, match[1])
			}
		}
		return extraction, nil
	}
	plan := filepath.Join(feature.Dir, PlanFile)
	if _, err := os.Lstat(plan); err != nil {
		return extraction, nil
	}
	content, err := ReadFile(plan)
	if err != nil {
		return extraction, err
	}
	block := structureBlock(splitLines(content))
	if block == nil {
		return extraction, nil
	}
	extraction.Source = protocol.SpecKitSourcePlan
	collector.addAll(treePaths(block))
	return extraction, nil
}

// taskTokens returns a task's candidate tokens: backtick-quoted spans, then
// whitespace-separated words, without the [P] and [USn] markers.
func taskTokens(text string) []string {
	text = taskMarker.ReplaceAllString(text, " ")
	var tokens []string
	for _, match := range backtickSpan.FindAllStringSubmatch(text, -1) {
		tokens = append(tokens, strings.TrimSpace(match[1]))
	}
	for _, word := range strings.Fields(backtickSpan.ReplaceAllString(text, " ")) {
		tokens = append(tokens, strings.TrimRight(strings.TrimLeft(word, `("'[*`), `.,;:)"'*]`))
	}
	return tokens
}

// structureBlock returns the lines of the first fenced block under a
// "Source Code" heading, or else under a "Project Structure" heading that is
// not the documentation tree.
func structureBlock(lines []string) []string {
	type block struct {
		heading string
		lines   []string
	}
	var blocks []block
	heading := ""
	for i := 0; i < len(lines); i++ {
		if anyHeading.MatchString(lines[i]) {
			heading = strings.ToLower(lines[i])
			continue
		}
		open := fence.FindStringSubmatch(lines[i])
		if open == nil {
			continue
		}
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), open[1]); i++ {
			body = append(body, lines[i])
		}
		blocks = append(blocks, block{heading: heading, lines: body})
	}
	for _, candidate := range blocks {
		if strings.Contains(candidate.heading, "source code") {
			return candidate.lines
		}
	}
	for _, candidate := range blocks {
		if strings.Contains(candidate.heading, "project structure") && !strings.Contains(candidate.heading, "documentation") {
			return candidate.lines
		}
	}
	return nil
}

// treePaths turns a directory tree into paths. Tree characters are stripped,
// and a name ending in "/" is the directory of its deeper-indented children.
func treePaths(lines []string) []string {
	type level struct {
		column int
		name   string
	}
	var stack []level
	var paths []string
	for _, line := range lines {
		column := 0
		rest := line
		for rest != "" {
			r, size := utf8.DecodeRuneInString(rest)
			if !strings.ContainsRune(treeChars, r) {
				break
			}
			rest = rest[size:]
			column++
		}
		if comment := strings.Index(rest, " #"); comment >= 0 {
			rest = rest[:comment]
		}
		name := strings.TrimSpace(rest)
		if name == "" || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "[") || strings.ContainsAny(name, " \t") {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].column >= column {
			stack = stack[:len(stack)-1]
		}
		parts := make([]string, 0, len(stack)+1)
		for _, parent := range stack {
			parts = append(parts, strings.TrimSuffix(parent.name, "/"))
		}
		if strings.HasSuffix(name, "/") {
			stack = append(stack, level{column: column, name: name})
			continue
		}
		paths = append(paths, strings.Join(append(parts, name), "/"))
	}
	return paths
}

type collector struct {
	root       string
	extraction *Extraction
	seen       map[string]bool
	unresolved map[string]bool
}

func newCollector(root string, extraction *Extraction) *collector {
	return &collector{root: root, extraction: extraction, seen: map[string]bool{}, unresolved: map[string]bool{}}
}

// addAll classifies tokens and returns how many were accepted paths,
// including ones accepted earlier.
func (c *collector) addAll(tokens []string) int {
	accepted := 0
	for _, token := range tokens {
		clean, state := classify(c.root, token)
		switch state {
		case pathExisting, pathNew:
			accepted++
			if c.seen[clean] {
				continue
			}
			if len(c.extraction.Paths) == MaxPaths {
				c.extraction.Truncated = true
				continue
			}
			c.seen[clean] = true
			c.extraction.Paths = append(c.extraction.Paths, clean)
			if state == pathNew {
				c.extraction.NewPaths = append(c.extraction.NewPaths, clean)
			}
		case pathUnresolved:
			if !c.unresolved[clean] && len(c.extraction.Unresolved) < MaxPaths {
				c.unresolved[clean] = true
				c.extraction.Unresolved = append(c.extraction.Unresolved, clean)
			}
		}
	}
	return accepted
}

type pathState int

const (
	notPath pathState = iota
	pathExisting
	pathNew
	pathUnresolved
)

// classify applies the path rules to one token. A token is a path when it
// contains "/" or ends in a file extension, has no scheme, is relative, does
// not traverse, and resolves inside root with symlinks evaluated. It is
// accepted when the file exists or its parent directory exists. A token
// without "/" is accepted only when the file exists, and a token without an
// extension only when it exists, so prose such as "e.g." or "and/or" never
// becomes a claim.
func classify(root, token string) (string, pathState) {
	token = strings.ReplaceAll(strings.TrimSpace(token), `\`, "/")
	if token == "" || len(token) > 1024 || strings.Contains(token, "://") || strings.ContainsAny(token, "\x00\r\n\t <>|\"*?") {
		return "", notPath
	}
	slashed := strings.Contains(strings.TrimSuffix(token, "/"), "/")
	extended := extension.MatchString(token)
	if !slashed && !extended {
		return "", notPath
	}
	if strings.HasPrefix(token, "/") || filepath.IsAbs(token) || filepath.VolumeName(filepath.FromSlash(token)) != "" || strings.HasPrefix(token, "~") {
		return "", notPath
	}
	for _, segment := range strings.Split(token, "/") {
		if segment == ".." {
			return "", notPath
		}
	}
	clean := path.Clean(token)
	if clean == "." || strings.HasSuffix(token, "/") {
		return "", notPath
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	if _, err := os.Lstat(target); err == nil {
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil || !inside(root, resolved) {
			return "", notPath
		}
		if stat, err := os.Stat(resolved); err != nil || !stat.Mode().IsRegular() {
			return "", notPath
		}
		return clean, pathExisting
	}
	if !slashed || !extended {
		return "", notPath
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return clean, pathUnresolved
	}
	if info, err := os.Stat(parent); err != nil || !info.IsDir() || !inside(root, parent) {
		return clean, pathUnresolved
	}
	return clean, pathNew
}

func inside(root, resolved string) bool {
	relative, err := filepath.Rel(root, resolved)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// CurrentFeature finds the feature Spec Kit considers current: the
// SPECIFY_FEATURE_DIRECTORY override, then feature_directory in
// .specify/feature.json, then specs/<branch>/. It returns "" when none
// applies. lookup reads the environment.
func CurrentFeature(root string, lookup func(string) (string, bool), branch string) (string, error) {
	candidate := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(root, filepath.FromSlash(value))
		}
		return value
	}
	if lookup != nil {
		if value, ok := lookup(FeatureDirectoryEnv); ok {
			if dir := candidate(value); dir != "" {
				return dir, nil
			}
		}
	}
	if content, err := ReadFile(filepath.Join(root, filepath.FromSlash(FeatureFile))); err == nil {
		var recorded struct {
			FeatureDirectory string `json:"feature_directory"`
		}
		if err := json.Unmarshal(content, &recorded); err != nil {
			return "", fmt.Errorf("%s is not valid JSON: %w", FeatureFile, err)
		}
		if dir := candidate(recorded.FeatureDirectory); dir != "" {
			return dir, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", FeatureFile, err)
	}
	if branch = strings.TrimSpace(branch); branch != "" && !strings.ContainsAny(branch, `/\`) && branch != "." && branch != ".." {
		dir := filepath.Join(root, "specs", branch)
		if IsFeaturePath(dir) {
			return dir, nil
		}
	}
	return "", nil
}

func splitLines(content []byte) []string {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), int(MaxFileBytes)+1)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSuffix(scanner.Text(), "\r"))
	}
	return lines
}

func statementText(text string) string {
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\x00", "")), " ")
	return bound(text, maxStatementRunes)
}

func bound(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}

func slug(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)))
	})
	key := strings.Join(words, "-")
	if key != "" && (key[0] < 'a' || key[0] > 'z') {
		key = "p-" + key
	}
	return key
}

func uniqueKey(key string, used map[string]bool) string {
	candidate := key
	for n := 2; used[candidate]; n++ {
		suffix := fmt.Sprintf("-%d", n)
		base := key
		if len(base)+len(suffix) > maxKeyLength {
			base = strings.TrimRight(base[:maxKeyLength-len(suffix)], "-")
		}
		candidate = base + suffix
	}
	return candidate
}
