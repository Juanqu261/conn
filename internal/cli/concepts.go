package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
)

func newConceptsCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "concepts",
		Short: "List the Project concept catalog of shared subjects",
		Long: "List Objective-derived subjects and the normalized subjects of active claims, with the\n" +
			"Intents that claim them. Use --query to rank by deterministic token similarity, so a new\n" +
			"Intent reuses an existing subject instead of inventing a different string.",
		Args: cobra.NoArgs,
	}
	r := newRemote(cmd, deps, streams)
	var projectID, objectiveID uuid.UUID
	var kind, query, after string
	var limit int
	requiredUUID(cmd, &projectID, "project", "project ID")
	cmd.Flags().Var(uuidValue{&objectiveID}, "objective", "only subjects derived from this Objective's current revision")
	cmd.Flags().StringVar(&kind, "kind", "", "subject kind filter, for example invariant or contract")
	cmd.Flags().StringVar(&query, "query", "", "task focus text used to rank subjects")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum concepts to return, from 1 to 200")
	cmd.Flags().StringVar(&after, "after", "", "continuation from a previous page")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 200 {
			return usageError{fmt.Errorf("--limit must be from 1 to 200")}
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.Concepts(cmd.Context(), projectID, httpclient.ConceptQuery{Kind: kind, Query: query, ObjectiveID: objectiveID, Limit: limit, After: after})
		if err != nil {
			return err
		}
		return printResult(r, result, func(out io.Writer, list protocol.ConceptList) {
			if len(list.Concepts) == 0 {
				fmt.Fprintln(out, "No concepts match.")
			}
			for _, concept := range list.Concepts {
				score := ""
				if concept.Score != nil {
					score = fmt.Sprintf("  score %.2f", *concept.Score)
				}
				fmt.Fprintf(out, "%s  %s  %d claim(s)  %s%s\n", concept.NormalizedSubject, concept.SubjectKind, concept.ClaimCount, strings.Join(concept.Sources, ","), score)
			}
			if list.HasMore {
				fmt.Fprintf(out, "More concepts remain; pass --after %s.\n", list.ContinuationAfter)
			}
			fmt.Fprintf(out, "As of event %d.\n", result.AsOfEvent)
		})
	}
	return cmd
}

func newSuggestClaimsCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest-claims",
		Short: "Suggest declared claims for files you plan to change, before editing",
		Long: "Run local planned analysis on the files you plan to change and merge the results with\n" +
			"Objective-derived subjects and catalog matches. Nothing is persisted. Publish the chosen\n" +
			"candidates as declared_claims with `conn intent publish` or `conn intent revise`.",
		Args: cobra.NoArgs,
	}
	r := newRemote(cmd, deps, streams)
	var projectID, objectiveID uuid.UUID
	var worktree, focus string
	var paths, symbols []string
	requiredUUID(cmd, &projectID, "project", "project ID")
	cmd.Flags().Var(uuidValue{&objectiveID}, "objective", "Objective whose invariants and acceptance criteria become candidates")
	cmd.Flags().StringSliceVar(&paths, "path", nil, "planned repository-relative path (repeatable, at most 50)")
	cmd.Flags().StringArrayVar(&symbols, "symbol", nil, "planned export as path=name (repeatable)")
	cmd.Flags().StringVar(&focus, "focus", "", "short task focus used to match catalog subjects")
	cmd.Flags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	_ = cmd.MarkFlagRequired("path")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		symbolMap := map[string][]string{}
		for _, value := range symbols {
			path, name, found := strings.Cut(value, "=")
			if !found || path == "" || name == "" {
				return usageError{fmt.Errorf("--symbol must be path=name")}
			}
			symbolMap[path] = append(symbolMap[path], name)
		}
		root, err := filepath.Abs(worktree)
		if err != nil {
			return usageError{err}
		}
		if root, err = filepath.EvalSymlinks(root); err != nil {
			return usageError{fmt.Errorf("resolve --worktree: %w", err)}
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		location := locateAnalyzer(deps)
		suggestions, err := preedit.SuggestClaims(cmd.Context(), preedit.SuggestDeps{
			Client: client, Analyzer: location.Config, AnalyzerAvailable: location.Available(),
		}, preedit.SuggestRequest{
			ProjectID: projectID, ObjectiveID: objectiveID, Worktree: root, Paths: paths, Symbols: symbolMap, Focus: focus,
		})
		if err != nil {
			return usageError{err}
		}
		if r.json {
			data, err := json.Marshal(suggestions)
			if err != nil {
				return err
			}
			writeEnvelope(streams.Out, data, suggestions.AsOfEvent)
			return nil
		}
		out := streams.Out
		for _, candidate := range suggestions.Candidates {
			fmt.Fprintf(out, "%-9s %-8s %s  (%s)\n", candidate.Source, candidate.Claim.Relation, candidate.Claim.NormalizedSubject, candidate.Claim.SubjectKind)
		}
		if len(suggestions.Candidates) == 0 {
			fmt.Fprintln(out, "No claim candidates were found.")
		}
		for _, path := range suggestions.NewPaths {
			fmt.Fprintf(out, "New planned file: %s\n", path)
		}
		if suggestions.OmittedCount > 0 {
			fmt.Fprintf(out, "%d candidate(s) were omitted at the bound.\n", suggestions.OmittedCount)
		}
		if !suggestions.Analysis.Available {
			fmt.Fprintf(out, "Planned analysis unavailable: %s\n", suggestions.Analysis.Reason)
		}
		fmt.Fprintln(out, "Nothing was persisted. Use --json to copy candidates into declared_claims.")
		return nil
	}
	return cmd
}

// locateAnalyzer returns the resolved local analyzer, or an unavailable
// location when the process supplies none.
func locateAnalyzer(deps Dependencies) analyzer.Location {
	if deps.Analyzer == nil {
		return analyzer.Location{Config: analyzer.Config{Timeout: analyzer.DefaultTimeout}}
	}
	return deps.Analyzer()
}
