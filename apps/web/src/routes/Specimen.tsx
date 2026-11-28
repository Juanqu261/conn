import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Tabs } from "radix-ui";
import type { ReactNode } from "react";
import {
  boardQuery,
  evidenceQuery,
  intentQuery,
  reviewMarkdownQuery,
  reviewPacketQuery,
} from "../api/queries";
import type {
  BoardCard,
  DriftSubject,
  EvidenceReference,
  IntentView,
  Lane,
  ReviewPacket,
  SemanticClaimCandidate,
} from "../api/types";
import { LANES } from "../board/lanes";
import { ExternalIcon } from "../components/icons";
import { useDocumentTitle } from "../components/title";
import {
  Avatar,
  CollisionBadge,
  EmptyState,
  EvidenceBadge,
  RelativeTime,
  ThemedLabel,
} from "../components/ui";
import { LABELS } from "../labels";
import { RULE_LABELS, summaryLine } from "../names/directory";
import { useProjectData } from "../project";
import {
  CopyButton,
  githubTreeUrl,
  MarkdownText,
  QueryState,
  shortCommit,
  useProjectId,
  useProjectSlug,
} from "./common";

export type SpecimenTab = "overview" | "claims" | "stitches" | "evidence" | "review";

const TABS: Array<{ value: SpecimenTab; text: string }> = [
  { value: "overview", text: "Overview" },
  { value: "claims", text: "Claims" },
  { value: "stitches", text: "Stitches" },
  { value: "evidence", text: "Evidence" },
  { value: "review", text: "Review packet" },
];

const PROVENANCE: Record<string, string> = {
  observed_run: "observed on a participant's machine",
  reported: "reported",
};

/**
 * The Specimen page: one Intent read like a pull request (requirement 6).
 * Every view is labelled with the revision it shows. The review packet is
 * generated on demand for the current revision and never stored.
 */
export function Specimen() {
  const projectId = useProjectId();
  const slug = useProjectSlug();
  const { intent: intentId } = useParams({ from: "/p/$project/intents/$intent" });
  const search = useSearch({ from: "/p/$project/intents/$intent" });
  const navigate = useNavigate({ from: "/p/$project/intents/$intent" });
  const tab = search.tab ?? "overview";
  const current = useQuery(intentQuery(projectId, intentId));
  const latest = current.data?.intent_revision.revision;
  const revision =
    search.revision && latest && search.revision < latest ? search.revision : undefined;
  const shown = useQuery({
    ...intentQuery(projectId, intentId, revision),
    enabled: Boolean(current.data),
  });
  const board = useQuery(boardQuery(projectId));
  const { names, radar } = useProjectData(projectId);
  const view = shown.data ?? current.data;
  useDocumentTitle(
    view ? summaryLine(view.intent_revision.summary_markdown) : LABELS.specimen.themed,
  );

  let card: BoardCard | undefined;
  let lane: Lane | undefined;
  for (const item of board.data?.lanes ?? []) {
    const found = item.cards.find((c) => c.intent_id === intentId);
    if (found) {
      card = found;
      lane = item.lane;
    }
  }

  return (
    <div className="page">
      <QueryState query={current} what="this Intent">
        {() => {
          if (!view || !latest) return null;
          const revisionData = view.intent_revision;
          const repository = radar.data?.repositories.find(
            (r) => r.repository_id === revisionData.repository_id,
          );
          const github = githubTreeUrl(repository, revisionData.branch_ref);
          const isCurrent = revisionData.revision === latest;
          return (
            <>
              <header className="specimen-header">
                <p className="eyebrow">
                  <ThemedLabel label={LABELS.specimen} />
                </p>
                <h1 className="page-title specimen-title">
                  {summaryLine(revisionData.summary_markdown)}
                </h1>
                {lane && card && (
                  <p className={`lane-badge lane-${lane}`}>
                    <strong>
                      <ThemedLabel label={LANES[lane]} />
                    </strong>{" "}
                    — {card.reason.facts_markdown}
                  </p>
                )}
                <p className="specimen-meta">
                  <Avatar human={names.humanRecord(view.owner_human_id)} size={22} />{" "}
                  {names.human(view.owner_human_id)} · {names.objective(revisionData.objective_id)}{" "}
                  rev {revisionData.objective_revision} ·{" "}
                  <span className="mono">
                    {revisionData.repository_id
                      ? names.repository(revisionData.repository_id)
                      : "No repository"}
                  </span>
                  {revisionData.branch_ref && (
                    <span className="mono"> · {revisionData.branch_ref}</span>
                  )}
                  {(revisionData.base_revision_ref || revisionData.head_revision_ref) && (
                    <span className="mono">
                      {" "}
                      · base {shortCommit(revisionData.base_revision_ref) || "unknown"} → head{" "}
                      {shortCommit(revisionData.head_revision_ref) || "unknown"}
                    </span>
                  )}
                  {view.status !== "active" && <span> · {view.status}</span>}
                </p>
                <div className="specimen-actions">
                  {github && (
                    <a
                      className="button button-secondary button-small"
                      href={github}
                      target="_blank"
                      rel="noreferrer noopener"
                    >
                      <ExternalIcon size={14} /> View on GitHub
                    </a>
                  )}
                  <label className="field">
                    <span>Revision</span>
                    <select
                      value={revisionData.revision}
                      onChange={(event) => {
                        const value = Number(event.target.value);
                        void navigate({
                          search: (s) => ({ ...s, revision: value === latest ? undefined : value }),
                          replace: true,
                        });
                      }}
                    >
                      {Array.from({ length: latest }, (_, i) => latest - i).map((value) => (
                        <option key={value} value={value}>
                          Revision {value}
                          {value === latest ? " (current)" : ""}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                {!isCurrent && (
                  <p className="notice notice-warning">
                    You are reading revision {revisionData.revision}. The current revision is{" "}
                    {latest}; lanes, stitches, and the review packet describe the current revision.
                  </p>
                )}
              </header>

              <Tabs.Root
                value={tab}
                onValueChange={(value) =>
                  void navigate({
                    search: (s) => ({ ...s, tab: value as SpecimenTab }),
                    replace: true,
                  })
                }
              >
                <Tabs.List className="tabs" aria-label="Intent views">
                  {TABS.map((item) => (
                    <Tabs.Trigger key={item.value} value={item.value} className="tab">
                      {item.text}
                      {item.value === "stitches" && card
                        ? ` ${card.open_collision_ids.length}`
                        : ""}
                    </Tabs.Trigger>
                  ))}
                </Tabs.List>
                <Tabs.Content value="overview" className="tab-panel">
                  <RevisionLabel revision={revisionData.revision} />
                  <Overview
                    view={view}
                    projectId={projectId}
                    intentId={intentId}
                    current={isCurrent}
                  />
                </Tabs.Content>
                <Tabs.Content value="claims" className="tab-panel">
                  <RevisionLabel revision={revisionData.revision} />
                  <Claims
                    view={view}
                    projectId={projectId}
                    intentId={intentId}
                    current={isCurrent}
                  />
                </Tabs.Content>
                <Tabs.Content value="stitches" className="tab-panel">
                  <RevisionLabel revision={latest} note="Stitches on the current revision" />
                  <Stitches projectId={projectId} intentId={intentId} slug={slug} />
                </Tabs.Content>
                <Tabs.Content value="evidence" className="tab-panel">
                  <RevisionLabel
                    revision={latest}
                    note="Evidence attached to this Intent, newest first"
                  />
                  <Evidence projectId={projectId} intentId={intentId} />
                </Tabs.Content>
                <Tabs.Content value="review" className="tab-panel">
                  <RevisionLabel revision={latest} note="Review packet of the current revision" />
                  <Packet projectId={projectId} intentId={intentId} />
                </Tabs.Content>
              </Tabs.Root>
            </>
          );
        }}
      </QueryState>
    </div>
  );
}

function RevisionLabel({ revision, note }: { revision: number; note?: string }) {
  return (
    <p className="revision-label">
      {note ?? "Showing"} · revision {revision}
    </p>
  );
}

function usePacket(projectId: string, intentId: string, enabled = true) {
  return useQuery({ ...reviewPacketQuery(projectId, intentId), enabled });
}

function Overview({
  view,
  projectId,
  intentId,
  current,
}: {
  view: IntentView;
  projectId: string;
  intentId: string;
  current: boolean;
}) {
  const packet = usePacket(projectId, intentId, current);
  const revision = view.intent_revision;
  return (
    <div className="specimen-grid">
      <section className="section" aria-labelledby="summary-title">
        <h2 id="summary-title" className="section-title">
          Summary
        </h2>
        <MarkdownText text={revision.summary_markdown} />
        <h3 className="subsection-title">Assumptions</h3>
        {revision.assumptions?.length ? (
          <ul className="statements">
            {revision.assumptions.map((assumption) => (
              <li key={assumption}>{assumption}</li>
            ))}
          </ul>
        ) : (
          <p className="muted">No assumptions declared.</p>
        )}
      </section>
      <section className="section" aria-labelledby="acceptance-title">
        <h2 id="acceptance-title" className="section-title">
          Acceptance criteria
        </h2>
        {!current ? (
          <p className="muted">Evidence states are reported for the current revision only.</p>
        ) : (
          <QueryState query={packet} what="the acceptance criteria">
            {() => <Acceptance packet={packet.data} />}
          </QueryState>
        )}
      </section>
    </div>
  );
}

function Acceptance({ packet }: { packet: ReviewPacket | undefined }) {
  if (!packet) return null;
  if (packet.acceptance.length === 0) {
    return (
      <p className="muted">
        The Objective revision has no keyed acceptance criteria, so no evidence maps to it.
      </p>
    );
  }
  const passing = packet.acceptance.filter(
    (item) => item.state === "passing" && !item.evidence[0]?.stale,
  ).length;
  return (
    <>
      <p className="row-meta">
        {passing} of {packet.acceptance.length} passing at the head commit
      </p>
      <ul className="checklist">
        {packet.acceptance.map((item) => {
          const latest = item.evidence[0];
          return (
            <li key={item.key} className="checklist-item">
              <EvidenceBadge state={latest?.stale ? "stale" : item.state} />
              <span>
                <code>{item.key}</code> {item.statement}
                <span className="row-meta">
                  {latest
                    ? ` ${PROVENANCE[latest.provenance] ?? latest.provenance}${latest.commit ? ` · ${shortCommit(latest.commit)}` : ""}${latest.stale ? " · stale against the head commit" : ""}`
                    : " no evidence from this Intent"}
                </span>
              </span>
            </li>
          );
        })}
      </ul>
    </>
  );
}

function Claims({
  view,
  projectId,
  intentId,
  current,
}: {
  view: IntentView;
  projectId: string;
  intentId: string;
  current: boolean;
}) {
  const packet = usePacket(projectId, intentId, current);
  const declared = view.intent_revision.declared_claims ?? [];
  return (
    <div className="specimen-grid">
      <section className="section" aria-labelledby="declared-title">
        <h2 id="declared-title" className="section-title">
          Declared ({declared.length})
        </h2>
        <ClaimList
          claims={declared}
          highlight={current ? packet.data?.drift.declared_not_observed : undefined}
          empty="No declared claims."
        />
      </section>
      <section className="section" aria-labelledby="observed-title">
        <h2 id="observed-title" className="section-title">
          Observed
        </h2>
        {!current ? (
          <p className="muted">Observed claims and drift describe the current revision only.</p>
        ) : (
          <QueryState query={packet} what="observed claims">
            {() => {
              const drift = packet.data?.drift;
              if (!drift) return null;
              if (drift.analysis.status === "not_analyzed") {
                return (
                  <p className="muted">
                    This revision was not analysed, so drift is not reported. This is not evidence
                    of no drift.
                  </p>
                );
              }
              return (
                <>
                  {drift.analysis.status === "incomplete" && (
                    <p className="notice notice-warning">
                      The observed analysis was incomplete; drift may miss changes.
                    </p>
                  )}
                  <ClaimList
                    claims={[...drift.matched, ...drift.observed_not_declared]}
                    highlight={drift.observed_not_declared}
                    empty="No observed claims."
                  />
                  <h3 className="subsection-title">Scope drift</h3>
                  <DriftList
                    title="Declared but not observed"
                    items={drift.declared_not_observed}
                  />
                  <DriftList
                    title="Observed but not declared"
                    items={drift.observed_not_declared}
                  />
                  <h3 className="subsection-title">Changed files without a declared claim</h3>
                  {drift.changed_files_without_claim.length === 0 ? (
                    <p className="muted">
                      None known. The server has no local Git facts, so changed files come from
                      observed analysis only.
                    </p>
                  ) : (
                    <ul className="statements mono">
                      {drift.changed_files_without_claim.map((file) => (
                        <li key={file.path} className="drift-flag">
                          {file.path}
                          {file.written_by_agent ? " · written by an agent" : ""}
                        </li>
                      ))}
                    </ul>
                  )}
                </>
              );
            }}
          </QueryState>
        )}
      </section>
    </div>
  );
}

function ClaimList({
  claims,
  highlight,
  empty,
}: {
  claims: Array<SemanticClaimCandidate | DriftSubject>;
  highlight: DriftSubject[] | undefined;
  empty: string;
}) {
  if (claims.length === 0) return <p className="muted">{empty}</p>;
  const flagged = new Set(highlight?.map((item) => `${item.normalized_subject}|${item.relation}`));
  return (
    <ul className="claim-list">
      {claims.map((claim) => {
        const key = `${claim.normalized_subject}|${claim.relation}`;
        return (
          <li key={key} className={flagged.has(key) ? "claim drift-flag" : "claim"}>
            <span className="claim-relation">{claim.relation.replace("_", " ")}</span>{" "}
            <code>{claim.normalized_subject}</code>
            <span className="row-meta"> {claim.display_name}</span>
            {flagged.has(key) && <span className="badge badge-warning">Drift</span>}
          </li>
        );
      })}
    </ul>
  );
}

function DriftList({ title, items }: { title: string; items: DriftSubject[] }) {
  return (
    <div className="drift-group">
      <p className="row-meta">
        {title}: {items.length === 0 ? "none" : items.length}
      </p>
      {items.length > 0 && (
        <ul className="statements">
          {items.map((item) => (
            <li key={`${item.normalized_subject}|${item.relation}`}>
              <code>{item.normalized_subject}</code> ({item.relation.replace("_", " ")})
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Stitches({
  projectId,
  intentId,
  slug,
}: {
  projectId: string;
  intentId: string;
  slug: string;
}) {
  const packet = usePacket(projectId, intentId);
  const { names } = useProjectData(projectId);
  return (
    <QueryState query={packet} what="the stitches">
      {() =>
        packet.data && packet.data.collisions.length > 0 ? (
          <ul className="list">
            {packet.data.collisions.map((collision) => {
              const known = names.collision(collision.id);
              return (
                <li key={collision.id} className="row">
                  <div className="row-main">
                    <Link
                      className="row-link"
                      to="/p/$project/stitches/$collision"
                      params={{ project: slug, collision: collision.id }}
                    >
                      {RULE_LABELS[collision.rule_id] ?? collision.rule_id}
                    </Link>
                    <CollisionBadge state={collision.state} />
                  </div>
                  <p className="row-meta">
                    {known ? names.collisionName(known) : "Decided or historical stitch"}
                  </p>
                </li>
              );
            })}
          </ul>
        ) : (
          <EmptyState title="No stitches">
            This revision collides with no other active work.
          </EmptyState>
        )
      }
    </QueryState>
  );
}

function Evidence({ projectId, intentId }: { projectId: string; intentId: string }) {
  const evidence = useQuery(evidenceQuery(projectId, intentId));
  return (
    <QueryState query={evidence} what="the evidence">
      {() =>
        evidence.data && evidence.data.evidence.length > 0 ? (
          <ol className="timeline">
            {evidence.data.evidence.map((item) => (
              <EvidenceItem key={item.evidence_id} item={item} />
            ))}
          </ol>
        ) : (
          <EmptyState title="No evidence yet">
            Run conn evidence run, or attach a reference, to add proof.
          </EmptyState>
        )
      }
    </QueryState>
  );
}

function EvidenceItem({ item }: { item: EvidenceReference }) {
  const observation = item.observation;
  const facts: ReactNode[] = [
    item.kind.replace("_", " "),
    item.acceptance_key ? <code key="key">{item.acceptance_key}</code> : "no acceptance key",
    observation
      ? observation.runner === "local"
        ? "observed on a participant's machine"
        : `observed by ${observation.runner}`
      : "reported",
  ];
  if (observation) {
    facts.push(
      <span key="commit" className="mono">
        commit {shortCommit(observation.commit)}
      </span>,
    );
    facts.push(
      <span key="hash" className="mono">
        output {observation.output_sha256.slice(0, 12)}
      </span>,
    );
  }
  return (
    <li className="timeline-item">
      <EvidenceBadge state={item.state} />
      <div>
        <p className="row-meta">
          {facts.map((fact, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: facts are positional
            <span key={i}>
              {i > 0 ? " · " : ""}
              {fact}
            </span>
          ))}{" "}
          · <RelativeTime value={item.observed_at} />
        </p>
        {item.summary && <p>{item.summary}</p>}
      </div>
    </li>
  );
}

function Packet({ projectId, intentId }: { projectId: string; intentId: string }) {
  const markdown = useQuery(reviewMarkdownQuery(projectId, intentId));
  return (
    <section aria-labelledby="packet-title">
      <div className="packet-header">
        <h2 id="packet-title" className="section-title">
          Review packet
        </h2>
        {markdown.data && <CopyButton text={markdown.data.text} label="Copy Markdown" />}
      </div>
      <p className="muted">
        Generated on demand from Conn records for this view and not stored. The server has no local
        Git facts, so run conn review in the worktree for branch and changed-file details.
      </p>
      <QueryState query={markdown} what="the review packet">
        {() => <pre className="packet">{markdown.data?.text}</pre>}
      </QueryState>
    </section>
  );
}
