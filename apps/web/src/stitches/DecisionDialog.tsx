import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Dialog } from "radix-ui";
import { useEffect, useId, useState } from "react";
import { ApiError, apiPost } from "../api/client";
import { collisionContextQuery, decisionsPath, projectKey, proposalsQuery } from "../api/queries";
import type {
  ConflictContextPack,
  IntentRevision,
  ResolutionDecision,
  ResolutionProposal,
  ResolutionProposalList,
  UUID,
} from "../api/types";
import { BoltIcon, CrossIcon } from "../components/icons";
import { Avatar, CollisionBadge, formatRelative, OutcomeBadge } from "../components/ui";
import { type Names, RULE_LABELS, summaryLine } from "../names/directory";
import { useProjectData } from "../project";
import { MarkdownText, SEVERITY_LABELS, useViewer } from "../routes/common";
import {
  ALWAYS_BINDING,
  CONSEQUENCES,
  type DecisionDraft,
  type DecisionTarget,
  decidable,
  decisionInput,
  draftProblem,
  initialDraft,
  isBinding,
  KIND_LABELS,
  kindsFor,
  TARGET_LABELS,
  TARGETS,
} from "./decision";

const ADAPTERS: Record<string, string> = {
  kiro: "agent-authored via Kiro",
  mcp: "agent-authored via an MCP client",
  cli: "written in the CLI",
  ci: "from CI",
  github: "from GitHub",
  other_registered_adapter: "from another adapter",
};

/** The author line of a proposal: who wrote it and whether an agent did. */
export function proposalAuthor(
  proposal: ResolutionProposal,
  list: ResolutionProposalList | undefined,
  names: Names,
): string {
  const author = list?.authors?.find(
    (item) => item.participant_id === proposal.proposed_by_participant_id,
  );
  if (author)
    return `${names.human(author.human_id)} · ${ADAPTERS[author.adapter_kind] ?? "from a participant"}`;
  if (proposal.proposed_by_human_id) return names.human(proposal.proposed_by_human_id);
  return "An unknown participant";
}

/**
 * The decision review dialog (requirements 5.2 to 5.6). It changes nothing
 * until the member confirms, then sends the exact revisions and state it
 * showed. A stale decision is refused by the backend; the dialog then shows
 * what changed on the current state and never resubmits by itself.
 */
export function DecisionDialog({
  projectId,
  collisionId,
  target,
  open,
  onOpenChange,
}: {
  projectId: UUID;
  collisionId: UUID;
  target?: DecisionTarget | undefined;
  open: boolean;
  onOpenChange(open: boolean): void;
}) {
  const queryClient = useQueryClient();
  const { names, radar } = useProjectData(projectId);
  const { me, role } = useViewer(projectId);
  const context = useQuery({ ...collisionContextQuery(projectId, collisionId), enabled: open });
  const proposals = useQuery({ ...proposalsQuery(projectId, collisionId), enabled: open });
  const [draft, setDraft] = useState<DecisionDraft>(() => initialDraft(target));
  const [stale, setStale] = useState<string>("");
  const [recorded, setRecorded] = useState<ResolutionDecision | undefined>();
  const ids = useId();

  useEffect(() => {
    if (open) {
      setDraft(initialDraft(target));
      setStale("");
      setRecorded(undefined);
    }
  }, [open, target]);

  const decide = useMutation({
    mutationFn: async (pack: ConflictContextPack) => {
      if (!draft.kind) throw new Error("Choose a decision kind.");
      return (
        await apiPost<ResolutionDecision>(
          decisionsPath(projectId, collisionId),
          decisionInput({ ...draft, kind: draft.kind }, pack.collision),
        )
      ).data;
    },
    onSuccess: (decision) => {
      setRecorded(decision);
      for (const kind of [
        "radar",
        "board",
        "collision",
        "collisions",
        "review",
        "intent",
      ] as const) {
        void queryClient.invalidateQueries({ queryKey: projectKey(kind, projectId) });
      }
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === "stale_decision") {
        // Show what changed and reload the current state; never resubmit.
        setStale(error.message);
        void queryClient.invalidateQueries({
          queryKey: collisionContextQuery(projectId, collisionId).queryKey,
        });
        void queryClient.invalidateQueries({
          queryKey: proposalsQuery(projectId, collisionId).queryKey,
        });
        void queryClient.invalidateQueries({ queryKey: projectKey("radar", projectId) });
      }
    },
  });

  const pack = context.data;
  const owner = role === "owner";
  const selected = proposals.data?.proposals.find(
    (proposal) => proposal.proposal_id === draft.proposalId,
  );
  const problem = draftProblem(draft, owner, selected?.kind);
  const project = radar.data?.project.name ?? names.project(projectId);
  const set = (patch: Partial<DecisionDraft>) => {
    decide.reset();
    setDraft((current) => ({ ...current, ...patch }));
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog dialog-wide" aria-describedby={`${ids}-desc`}>
          <header className="dialog-header">
            <Dialog.Title className="dialog-title">Decide this stitch</Dialog.Title>
            <Dialog.Close className="icon-button" aria-label="Close without deciding">
              <CrossIcon />
            </Dialog.Close>
          </header>
          <Dialog.Description id={`${ids}-desc`} className="dialog-description">
            Nothing changes until you record the decision. It is attributed to you, from the
            browser, at the revisions shown here.
          </Dialog.Description>

          {recorded ? (
            <div className="notice notice-alive" role="status">
              <p>
                Recorded a {recorded.binding ? "binding" : "non-binding"}{" "}
                {KIND_LABELS[recorded.kind].toLowerCase()} decision in {project}. Kiro sessions see
                it on their next prompt.
              </p>
              <Dialog.Close className="button button-secondary">Close</Dialog.Close>
            </div>
          ) : context.isPending ? (
            <p className="muted" role="status">
              Reading the stitch…
            </p>
          ) : context.isError || !pack ? (
            <p className="notice notice-failing" role="alert">
              {context.error?.message ?? "Conn could not read this stitch."}
            </p>
          ) : (
            <form
              className="decision-form"
              onSubmit={(event) => {
                event.preventDefault();
                if (!problem && decidable(pack.collision.state)) {
                  setStale("");
                  decide.mutate(pack);
                }
              }}
            >
              {stale && (
                <div className="notice notice-warning" role="alert">
                  <p>{stale}</p>
                  <p>
                    The dialog now shows the current state. Review it and record the decision again
                    if it still holds.
                  </p>
                </div>
              )}
              <p className="decision-rule">
                <strong>{project}</strong> ·{" "}
                {RULE_LABELS[pack.collision.rule_id] ?? pack.collision.rule_id} ·{" "}
                {SEVERITY_LABELS[pack.collision.severity]} severity · confidence{" "}
                {pack.collision.confidence.toFixed(2)} ·{" "}
                <CollisionBadge state={pack.collision.state} />
              </p>
              <StitchSides pack={pack} names={names} />
              <MarkdownText
                className="decision-explanation"
                text={
                  pack.collision.explanation_markdown ||
                  "No explanation was recorded for this rule match."
                }
              />

              <PairCheck pack={pack} />

              <fieldset className="choice-group">
                <legend>Proposals (non-binding)</legend>
                {proposals.isPending && <p className="muted">Reading proposals…</p>}
                {proposals.data?.proposals.length === 0 && (
                  <p className="muted">No proposals yet. A proposal never decides anything.</p>
                )}
                <ol className="proposal-list">
                  {proposals.data?.proposals.map((proposal) => {
                    const based = draft.proposalId === proposal.proposal_id;
                    return (
                      <li
                        key={proposal.proposal_id}
                        className={based ? "proposal proposal-based" : "proposal"}
                      >
                        <p className="row-meta">
                          Suggests {KIND_LABELS[proposal.kind].toLowerCase()} ·{" "}
                          {proposalAuthor(proposal, proposals.data, names)} ·{" "}
                          {formatRelative(proposal.created_at)}
                        </p>
                        <MarkdownText text={proposal.rationale_markdown} />
                        <button
                          type="button"
                          className="button button-secondary button-small"
                          aria-pressed={based}
                          onClick={() =>
                            set({ proposalId: based ? undefined : proposal.proposal_id })
                          }
                        >
                          {based ? "Stop basing on this proposal" : "Base on this proposal"}
                        </button>
                      </li>
                    );
                  })}
                </ol>
              </fieldset>

              <fieldset className="choice-group">
                <legend>Move the stitch to</legend>
                <div className="choice-row">
                  {TARGETS.map((value) => (
                    <label key={value} className="choice">
                      <input
                        type="radio"
                        name={`${ids}-target`}
                        value={value}
                        checked={draft.target === value}
                        onChange={() => set({ target: value, kind: undefined })}
                      />
                      {TARGET_LABELS[value]}
                    </label>
                  ))}
                </div>
              </fieldset>

              {draft.target && (
                <fieldset className="choice-group">
                  <legend>Decision</legend>
                  {kindsFor(draft.target).map((kind) => (
                    <label key={kind} className="choice choice-described">
                      <input
                        type="radio"
                        name={`${ids}-kind`}
                        value={kind}
                        checked={draft.kind === kind}
                        onChange={() => set({ kind })}
                      />
                      <span>
                        <strong>{KIND_LABELS[kind]}</strong>
                        <span className="choice-description">{CONSEQUENCES[kind]}</span>
                      </span>
                    </label>
                  ))}
                </fieldset>
              )}

              {draft.kind && !ALWAYS_BINDING.has(draft.kind) && owner && (
                <fieldset className="choice-group">
                  <legend>Authority</legend>
                  <label className="choice">
                    <input
                      type="radio"
                      name={`${ids}-binding`}
                      checked={draft.binding === "record"}
                      onChange={() => set({ binding: "record" })}
                    />
                    Non-binding record
                  </label>
                  <label className="choice">
                    <input
                      type="radio"
                      name={`${ids}-binding`}
                      checked={draft.binding === "binding"}
                      onChange={() => set({ binding: "binding" })}
                    />
                    Binding team decision
                  </label>
                </fieldset>
              )}
              {draft.kind && (
                <p className="row-meta">
                  This will be a {isBinding(draft.kind, draft.binding) ? "binding" : "non-binding"}{" "}
                  decision
                  {ALWAYS_BINDING.has(draft.kind)
                    ? ", because this kind always binds the team"
                    : ""}
                  .
                  {!owner && draft.kind === "false_positive" && me && !behind(pack, names, me)
                    ? " Only the owner or the Human behind one of these Intents can dismiss it."
                    : ""}
                </p>
              )}

              <label className="field field-block">
                <span>Rationale (required)</span>
                <textarea
                  required
                  maxLength={4000}
                  rows={4}
                  value={draft.rationale}
                  onChange={(event) => set({ rationale: event.target.value })}
                />
              </label>

              {decide.isError &&
                !(decide.error instanceof ApiError && decide.error.code === "stale_decision") && (
                  <p className="notice notice-failing" role="alert">
                    {decide.error.message}
                  </p>
                )}
              {!decidable(pack.collision.state) && (
                <p className="notice notice-warning" role="status">
                  This stitch is now {pack.collision.state.replace("_", " ")}; it changes only
                  through a new decision on a current stitch or a new Intent revision.
                </p>
              )}
              <footer className="dialog-footer">
                {problem && <p className="muted dialog-problem">{problem}</p>}
                <Dialog.Close className="button button-secondary" type="button">
                  Cancel
                </Dialog.Close>
                <button
                  type="submit"
                  className="button button-primary"
                  disabled={
                    Boolean(problem) || decide.isPending || !decidable(pack.collision.state)
                  }
                >
                  <BoltIcon size={16} /> {decide.isPending ? "Recording…" : "Record decision"}
                </button>
              </footer>
            </form>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function behind(pack: ConflictContextPack, names: Names, me: UUID): boolean {
  return pack.intents.some((intent) => names.intentOwner(intent.intent_id) === me);
}

/** Both Intents at their exact revisions, with the matched subjects between them. */
export function StitchSides({ pack, names }: { pack: ConflictContextPack; names: Names }) {
  const left = pack.intents.find((intent) => intent.intent_id === pack.collision.left_intent_id);
  const right = pack.intents.find((intent) => intent.intent_id === pack.collision.right_intent_id);
  return (
    <div className="stitch-sides">
      <SideCard intent={left} names={names} revision={pack.collision.left_intent_revision} />
      <ul className="stitch-subjects" aria-label="Matched subjects">
        {pack.collision.matched_subjects.map((subject) => (
          <li key={subject} className="stitch-subject">
            <span className="stitch-line" aria-hidden="true" />
            <code>{subject}</code>
            <span className="stitch-line" aria-hidden="true" />
          </li>
        ))}
      </ul>
      <SideCard intent={right} names={names} revision={pack.collision.right_intent_revision} />
    </div>
  );
}

function SideCard({
  intent,
  names,
  revision,
}: {
  intent: IntentRevision | undefined;
  names: Names;
  revision: number;
}) {
  const owner = intent ? names.intentOwner(intent.intent_id) : undefined;
  return (
    <div className="stitch-side">
      <p className="stitch-side-owner">
        <Avatar human={names.humanRecord(owner)} size={20} /> {names.human(owner)}
      </p>
      <p className="stitch-side-name">
        {intent ? summaryLine(intent.summary_markdown) : names.intent(undefined)}
      </p>
      <p className="row-meta">Revision {revision}</p>
    </div>
  );
}

export function PairCheck({ pack }: { pack: ConflictContextPack }) {
  const latest = pack.combination_checks?.[0];
  if (!latest) {
    return (
      <p className="row-meta">
        Pair check: not run yet. Run conn verify-pair locally to combine both branches.
      </p>
    );
  }
  return (
    <p className="row-meta">
      Pair check: <OutcomeBadge outcome={latest.outcome} />
      {latest.diagnostics.ran ? ` · ${latest.diagnostics.new_count} new diagnostics` : ""} ·{" "}
      {formatRelative(latest.created_at)}
    </p>
  );
}
