import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { collisionContextQuery, decisionQuery, proposalsQuery } from "../api/queries";
import type { ConflictContextPack, IntentRevision, SemanticClaim } from "../api/types";
import { useDocumentTitle } from "../components/title";
import {
  Avatar,
  CollisionBadge,
  formatRelative,
  OutcomeBadge,
  RelativeTime,
  ThemedLabel,
} from "../components/ui";
import { LABELS } from "../labels";
import { type Names, RULE_LABELS, summaryLine } from "../names/directory";
import { useProjectData } from "../project";
import { DecisionDialog, PairCheck, proposalAuthor } from "../stitches/DecisionDialog";
import { decidable, KIND_LABELS } from "../stitches/decision";
import {
  CopyButton,
  MarkdownText,
  QueryState,
  SEVERITY_LABELS,
  shortCommit,
  useProjectId,
  useProjectSlug,
} from "./common";

/**
 * The Stitch page: both sides of one collision next to each other
 * (requirement 7). Proposals are shown as non-binding and never preselect
 * the decision.
 */
export function StitchPage() {
  const projectId = useProjectId();
  const { collision: collisionId } = useParams({ from: "/p/$project/stitches/$collision" });
  const context = useQuery(collisionContextQuery(projectId, collisionId));
  const { names } = useProjectData(projectId);
  const [deciding, setDeciding] = useState(false);
  useDocumentTitle(
    context.data ? names.collisionName(context.data.collision) : LABELS.stitch.themed,
  );

  return (
    <div className="page">
      <QueryState query={context} what="this stitch">
        {() => {
          const pack = context.data;
          if (!pack) return null;
          const collision = pack.collision;
          return (
            <>
              <header className="specimen-header">
                <p className="eyebrow">
                  <ThemedLabel label={LABELS.stitch} />
                </p>
                <h1 className="page-title">
                  {RULE_LABELS[collision.rule_id] ?? collision.rule_id}
                </h1>
                <p className="specimen-meta">
                  <CollisionBadge state={collision.state} /> {SEVERITY_LABELS[collision.severity]}{" "}
                  severity · confidence {collision.confidence.toFixed(2)} · opened{" "}
                  <RelativeTime value={collision.created_at} /> · governed by {pack.objective.title}{" "}
                  rev {pack.objective.revision}
                </p>
                {decidable(collision.state) && (
                  <div className="specimen-actions">
                    <button
                      type="button"
                      className="button button-primary"
                      onClick={() => setDeciding(true)}
                    >
                      Decide…
                    </button>
                  </div>
                )}
              </header>

              <Sides pack={pack} names={names} />

              <section className="section" aria-labelledby="rule-title">
                <h2 id="rule-title" className="section-title">
                  Why Conn stitched these
                </h2>
                <MarkdownText
                  text={
                    collision.explanation_markdown ||
                    "No explanation was recorded for this rule match."
                  }
                />
                {pack.uncertainties.length > 0 && (
                  <ul className="statements muted">
                    {pack.uncertainties.slice(0, 6).map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                )}
              </section>

              <Proposals projectId={projectId} collisionId={collisionId} names={names} />
              <History pack={pack} projectId={projectId} names={names} />
              <Checks pack={pack} names={names} />

              {deciding && (
                <DecisionDialog
                  projectId={projectId}
                  collisionId={collisionId}
                  open
                  onOpenChange={setDeciding}
                />
              )}
            </>
          );
        }}
      </QueryState>
    </div>
  );
}

/** Both Intents side by side; matched subjects are drawn as stitches between their claims. */
function Sides({ pack, names }: { pack: ConflictContextPack; names: Names }) {
  const slug = useProjectSlug();
  const matched = new Set(pack.collision.matched_subjects);
  const side = (intentId: string, revision: number) => ({
    intent: pack.intents.find((intent) => intent.intent_id === intentId),
    revision,
    claims: pack.claims.filter((claim) => claim.intent_id === intentId),
  });
  const left = side(pack.collision.left_intent_id, pack.collision.left_intent_revision);
  const right = side(pack.collision.right_intent_id, pack.collision.right_intent_revision);
  return (
    <section className="section" aria-labelledby="sides-title">
      <h2 id="sides-title" className="section-title">
        Both sides
      </h2>
      <div className="stitch-page-sides">
        <Side {...left} names={names} matched={matched} slug={slug} />
        <ul className="stitch-subjects" aria-label="Matched subjects">
          {pack.collision.matched_subjects.map((subject) => (
            <li key={subject} className="stitch-subject">
              <span className="stitch-line" aria-hidden="true" />
              <code>{subject}</code>
              <span className="stitch-line" aria-hidden="true" />
            </li>
          ))}
        </ul>
        <Side {...right} names={names} matched={matched} slug={slug} />
      </div>
    </section>
  );
}

function Side({
  intent,
  revision,
  claims,
  names,
  matched,
  slug,
}: {
  intent: IntentRevision | undefined;
  revision: number;
  claims: SemanticClaim[];
  names: Names;
  matched: Set<string>;
  slug: string;
}) {
  const owner = intent ? names.intentOwner(intent.intent_id) : undefined;
  return (
    <div className="stitch-side">
      <p className="stitch-side-owner">
        <Avatar human={names.humanRecord(owner)} size={22} /> {names.human(owner)}
      </p>
      {intent ? (
        <Link
          className="stitch-side-name"
          to="/p/$project/intents/$intent"
          params={{ project: slug, intent: intent.intent_id }}
        >
          {summaryLine(intent.summary_markdown)}
        </Link>
      ) : (
        <p className="stitch-side-name">{names.intent(undefined)}</p>
      )}
      <p className="row-meta">
        Revision {revision}
        {intent?.branch_ref ? ` · ${intent.branch_ref}` : ""}
      </p>
      <ul className="claim-list">
        {claims.map((claim) => (
          <li
            key={claim.claim_id}
            className={matched.has(claim.normalized_subject) ? "claim claim-matched" : "claim"}
          >
            <span className="claim-relation">{claim.relation.replace("_", " ")}</span>{" "}
            <code>{claim.normalized_subject}</code>
            <span className="row-meta"> {claim.provenance_kind.replace("_", " ")}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Proposals({
  projectId,
  collisionId,
  names,
}: {
  projectId: string;
  collisionId: string;
  names: Names;
}) {
  const proposals = useQuery(proposalsQuery(projectId, collisionId));
  return (
    <section className="section" aria-labelledby="proposals-title">
      <h2 id="proposals-title" className="section-title">
        Proposals
      </h2>
      <p className="muted">Proposals are non-binding. Only a human decision changes the stitch.</p>
      <QueryState query={proposals} what="the proposals">
        {() =>
          proposals.data && proposals.data.proposals.length > 0 ? (
            <ol className="proposal-list">
              {proposals.data.proposals.map((proposal) => (
                <li key={proposal.proposal_id} className="proposal">
                  <p className="row-meta">
                    Suggests {KIND_LABELS[proposal.kind].toLowerCase()} ·{" "}
                    {proposalAuthor(proposal, proposals.data, names)} ·{" "}
                    {formatRelative(proposal.created_at)}
                  </p>
                  <MarkdownText text={proposal.rationale_markdown} />
                </li>
              ))}
            </ol>
          ) : (
            <p className="muted">No proposals yet.</p>
          )
        }
      </QueryState>
    </section>
  );
}

function History({
  pack,
  projectId,
  names,
}: {
  pack: ConflictContextPack;
  projectId: string;
  names: Names;
}) {
  const refs = pack.existing_decision_refs ?? [];
  const decisions = useQueries({ queries: refs.map((ref) => decisionQuery(projectId, ref)) });
  return (
    <section className="section" aria-labelledby="history-title">
      <h2 id="history-title" className="section-title">
        Decision history
      </h2>
      {refs.length === 0 ? (
        <p className="muted">No human has decided this stitch yet.</p>
      ) : (
        <ol className="timeline">
          {decisions.map((query, i) => (
            <li key={refs[i]} className="timeline-item">
              {query.data ? (
                <div>
                  <p className="row-meta">
                    {query.data.binding ? "Binding" : "Non-binding"}{" "}
                    {KIND_LABELS[query.data.kind].toLowerCase()} by{" "}
                    {names.human(query.data.decided_by_human_id)} ·{" "}
                    <RelativeTime value={query.data.created_at} />
                  </p>
                  <MarkdownText text={query.data.rationale_markdown} />
                </div>
              ) : (
                <p className="muted">
                  {query.isError ? "This decision could not be read." : "Reading…"}
                </p>
              )}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function Checks({ pack, names }: { pack: ConflictContextPack; names: Names }) {
  const command = `conn verify-pair ${pack.collision.collision_id}`;
  const checks = pack.combination_checks ?? [];
  return (
    <section className="section" aria-labelledby="checks-title">
      <h2 id="checks-title" className="section-title">
        Pair checks
      </h2>
      <PairCheck pack={pack} />
      {checks.length > 0 && (
        <ol className="timeline">
          {checks.map((check) => (
            <li key={check.check_id} className="timeline-item">
              <OutcomeBadge outcome={check.outcome} />
              <p className="row-meta">
                {names.intent(check.left.intent_id)} at{" "}
                <span className="mono">{shortCommit(check.left.commit)}</span> with{" "}
                {names.intent(check.right.intent_id)} at{" "}
                <span className="mono">{shortCommit(check.right.commit)}</span>
                {check.diagnostics.ran ? ` · ${check.diagnostics.new_count} new diagnostics` : ""} ·{" "}
                <RelativeTime value={check.created_at} />
              </p>
            </li>
          ))}
        </ol>
      )}
      <div className="command-box">
        <p className="row-meta">
          Combine both branches on your machine. It runs code from both branches after you confirm.
        </p>
        <pre className="mono command">{command}</pre>
        <CopyButton text={command} label="Copy command" />
      </div>
    </section>
  );
}
