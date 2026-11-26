package review

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/repositorygit"
)

// Trailer log bounds. A range with more commits is read up to the bound.
const (
	maxTrailerCommits = 1000
	maxTrailerLog     = 8 << 20
)

var (
	trailerPattern = regexp.MustCompile(`(?i)^conn-intent:[ \t]*([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})@([1-9][0-9]{0,9})[ \t]*$`)
	revisionRef    = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// IntentTrailers is what a commit range says about one Intent: the highest
// revision named and the commits that named it, newest first.
type IntentTrailers struct {
	IntentID       uuid.UUID
	LatestRevision int
	Commits        []string
}

// TrailerLog is the parsed Conn-Intent trailers of a commit range.
type TrailerLog struct {
	// Commits lists every commit in the range, newest first.
	Commits  []string
	ByIntent map[uuid.UUID]IntentTrailers
}

// ParseTrailer parses one `Conn-Intent: <intent-id>@<revision>` line.
func ParseTrailer(line string) (uuid.UUID, int, bool) {
	match := trailerPattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return uuid.UUID{}, 0, false
	}
	id, err := uuid.Parse(match[1])
	if err != nil {
		return uuid.UUID{}, 0, false
	}
	revision, err := strconv.Atoi(match[2])
	if err != nil || revision < 1 {
		return uuid.UUID{}, 0, false
	}
	return id, revision, true
}

// ParseTrailerLog parses the output of
// `git log --format=%H%x00%B%x00 <range>`. Only the last paragraph of each
// message, where Git keeps trailers, is read.
func ParseTrailerLog(output string) TrailerLog {
	log := TrailerLog{Commits: []string{}, ByIntent: map[uuid.UUID]IntentTrailers{}}
	fields := strings.Split(output, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		commit := strings.TrimSpace(fields[i])
		if !revisionRef.MatchString(commit) {
			continue
		}
		log.Commits = append(log.Commits, commit)
		named := map[uuid.UUID]bool{}
		for _, line := range strings.Split(lastParagraph(fields[i+1]), "\n") {
			id, revision, ok := ParseTrailer(line)
			if !ok {
				continue
			}
			entry := log.ByIntent[id]
			entry.IntentID = id
			entry.LatestRevision = max(entry.LatestRevision, revision)
			if !named[id] {
				named[id] = true
				entry.Commits = append(entry.Commits, commit)
			}
			log.ByIntent[id] = entry
		}
	}
	return log
}

// ReadTrailers reads the Conn-Intent trailers of base..head from local Git.
// Revisions must be commit IDs or safe branch names.
func ReadTrailers(ctx context.Context, worktree, base, head string) (TrailerLog, error) {
	if !safeRevision(base) || !safeRevision(head) {
		return TrailerLog{}, errUnsafeRevision
	}
	output, err := repositorygit.Output(ctx, worktree, maxTrailerLog, "log", "--max-count="+strconv.Itoa(maxTrailerCommits), "--format=%H%x00%B%x00", base+".."+head, "--")
	if err != nil {
		return TrailerLog{}, err
	}
	return ParseTrailerLog(output), nil
}

func lastParagraph(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	if index := strings.LastIndex(message, "\n\n"); index >= 0 {
		return message[index+2:]
	}
	return message
}

func safeRevision(ref string) bool {
	return revisionRef.MatchString(ref) || ref == "HEAD" || repositorygit.ValidBranchName(ref)
}
