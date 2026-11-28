import { useParams, useSearch } from "@tanstack/react-router";
import { type ReactNode, useEffect } from "react";
import type {
  CollisionAssessment,
  EvidenceCoverage,
  RadarObjective,
  RadarView,
  UUID,
} from "../api/types";
import {
  BoltIcon,
  FlaskIcon,
  FogIcon,
  JarIcon,
  QuestionIcon,
  StitchIcon,
} from "../components/icons";
import { useDocumentTitle } from "../components/title";
import {
  Avatar,
  CollisionBadge,
  CopyIdMenu,
  EmptyState,
  EvidenceBadge,
  formatRelative,
  OutcomeBadge,
  ThemedLabel,
  useNow,
} from "../components/ui";
import { LABELS, type Label } from "../labels";
import { type Names, projectIdFromSlug, RULE_LABELS } from "../names/directory";
import { useProjectData } from "../project";

const SOURCE_KINDS: Record<NonNullable<RadarObjective["source_kind"]>, string> = {
  kiro_spec: "Kiro spec",
  speckit_feature: "Spec Kit feature",
  github_issue: "GitHub issue",
};

const SEVERITY: Record<CollisionAssessment["severity"], string> = {
  info: "Info severity",
  amber: "Amber severity",
  red: "Red severity",
};

/**
 * The Lab: the state of the Project in under a minute (requirement 6).
 * Every figure links to the section that explains it. Nothing here counts
 * or ranks people.
 */
export function Overview() {
  const { project: slug } = useParams({ from: "/p/$project" });
  const { focus } = useSearch({ from: "/p/$project/" });
  const projectId = projectIdFromSlug(slug);
  const { radar, names } = useProjectData(projectId);
  const now = useNow();
  useDocumentTitle(
    radar.data ? `${LABELS.lab.themed} · ${radar.data.project.name}` : LABELS.lab.themed,
  );

  useEffect(() => {
    if (!focus || !radar.data) return;
    const element = document.getElementById(focus.replace(":", "-"));
    if (!element) return;
    element.scrollIntoView({ block: "center" });
    element.classList.add("focused");
    element.focus({ preventScroll: true });
    const timer = window.setTimeout(() => element.classList.remove("focused"), 2400);
    return () => window.clearTimeout(timer);
  }, [focus, radar.data]);

  if (radar.isPending) {
    return (
      <div className="page" role="status">
        <p className="muted">Reading the Lab…</p>
      </div>
    );
  }
  if (radar.isError) {
    return (
      <div className="page">
        <div className="notice notice-failing" role="alert">
          <p>{radar.error.message}</p>
          <button
            type="button"
            className="button button-secondary"
            onClick={() => void radar.refetch()}
          >
            Try again
          </button>
        </div>
      </div>
    );
  }

  const view = radar.data;
  const collisions = view.open_collisions;
  const pairTrouble = collisions.filter(
    (collision) =>
      collision.latest_combination_outcome && collision.latest_combination_outcome !== "passing",
  );
  const underReview = collisions.filter((collision) => collision.state === "under_review").length;

  return (
    <div className="page">
      <header className="page-header">
        <ThemedLabel label={LABELS.lab} as="h1" className="page-title" />
        <p className="as-of">
          As of event {view.as_of_event.toLocaleString()} · read{" "}
          {formatRelative(radar.dataUpdatedAt, now)}
        </p>
      </header>

      <ul className="tiles" aria-label="Project at a glance">
        <Tile
          href="#objectives"
          icon={<FlaskIcon />}
          value={view.objectives.length}
          label={LABELS.objectives}
        />
        <Tile
          href="#specimens"
          icon={<JarIcon />}
          value={view.active_intents.length}
          label={LABELS.specimens}
        />
        <Tile
          href="#stitches"
          icon={<StitchIcon />}
          value={collisions.length}
          label={LABELS.stitches}
          detail={underReview > 0 ? `${underReview} under review` : undefined}
          tone={collisions.length > 0 ? "warning" : undefined}
        />
        <Tile
          href="#stitches"
          icon={<BoltIcon />}
          value={pairTrouble.length}
          label={LABELS.pairChecks}
          tone={pairTrouble.length > 0 ? "failing" : undefined}
        />
        <Tile
          href="#questions"
          icon={<QuestionIcon />}
          value={view.open_questions.length}
          label={LABELS.questions}
        />
        <Tile
          href="#unknowns"
          icon={<FogIcon />}
          value={view.known_unknowns.length}
          label={LABELS.unknowns}
        />
      </ul>

      <Section id="objectives" label={LABELS.objectives}>
        {view.objectives.length === 0 ? (
          <EmptyState title="No blueprints on the wall.">
            Objectives arrive when someone imports a Kiro spec, a Spec Kit feature, or a GitHub
            issue with <code className="mono">conn start</code>.
          </EmptyState>
        ) : (
          <ul className="list">
            {view.objectives.map((objective) => (
              <ObjectiveRow
                key={objective.objective_id}
                objective={objective}
                coverage={view.evidence_coverage}
              />
            ))}
          </ul>
        )}
      </Section>

      <Section id="specimens" label={LABELS.specimens}>
        {view.active_intents.length === 0 ? (
          <EmptyState title="The slab is empty. No Intent is being assembled yet.">
            Work enters the Lab when someone runs <code className="mono">/conn-start</code> in Kiro
            or <code className="mono">conn start</code> in a terminal. It appears here the moment it
            is published.
          </EmptyState>
        ) : (
          <ul className="list">
            {view.active_intents.map((intent) => (
              <IntentRow key={intent.intent_id} intent={intent} view={view} names={names} />
            ))}
          </ul>
        )}
      </Section>

      <Section id="stitches" label={LABELS.stitches}>
        {collisions.length === 0 ? (
          <EmptyState title="No open stitches.">
            No two Intents change the same behavior, contract, or data right now.
          </EmptyState>
        ) : (
          <ul className="list">
            {collisions.map((collision) => (
              <CollisionRow key={collision.collision_id} collision={collision} names={names} />
            ))}
          </ul>
        )}
      </Section>

      <div className="section-pair">
        <Section id="questions" label={LABELS.questions}>
          <StatementList items={view.open_questions} empty="No open questions." />
        </Section>
        <Section id="unknowns" label={LABELS.unknowns}>
          <StatementList items={view.known_unknowns} empty="Nothing known to be unknown." />
        </Section>
      </div>
    </div>
  );
}

function Tile({
  href,
  icon,
  value,
  label,
  detail,
  tone,
}: {
  href: string;
  icon: ReactNode;
  value: number;
  label: Label;
  detail?: string | undefined;
  tone?: "warning" | "failing" | undefined;
}) {
  return (
    <li>
      <a href={href} className={tone ? `tile tile-${tone}` : "tile"}>
        <span className="tile-icon">{icon}</span>
        <span className="tile-value">{value.toLocaleString()}</span>
        <ThemedLabel label={label} className="tile-label" />
        {detail && <span className="tile-detail">{detail}</span>}
      </a>
    </li>
  );
}

function Section({ id, label, children }: { id: string; label: Label; children: ReactNode }) {
  return (
    <section className="section" aria-labelledby={`${id}-heading`}>
      <header className="section-header" id={`${id}-heading`}>
        <span id={id} className="anchor" />
        <ThemedLabel label={label} as="h2" className="section-title" />
      </header>
      {children}
    </section>
  );
}

function ObjectiveRow({
  objective,
  coverage,
}: {
  objective: RadarObjective;
  coverage: EvidenceCoverage[];
}) {
  const criteria = coverage.filter(
    (entry) =>
      entry.objective_id === objective.objective_id &&
      entry.objective_revision === objective.revision,
  );
  const passing = criteria.filter((entry) => entry.state === "passing" && !entry.stale).length;
  return (
    <li className="row" id={`objective-${objective.objective_id}`} tabIndex={-1}>
      <div className="row-main">
        <p className="row-title">{objective.title}</p>
        <p className="row-meta">
          Revision {objective.revision}
          {objective.source_kind && ` · from a ${SOURCE_KINDS[objective.source_kind]}`}
        </p>
        {criteria.length === 0 ? (
          <p className="row-meta">No acceptance criteria are tracked for this revision.</p>
        ) : (
          <>
            <div className="coverage">
              <meter
                className="coverage-meter"
                min={0}
                max={criteria.length}
                value={passing}
                aria-label={`${passing} of ${criteria.length} acceptance criteria proven`}
              />
              <span className="coverage-text">
                {passing} of {criteria.length} proven
              </span>
            </div>
            <ul className="key-chips" aria-label="Acceptance criteria">
              {criteria.map((entry) => (
                <li key={entry.acceptance_key} className="key-chip">
                  <code className="mono">{entry.acceptance_key}</code>
                  <EvidenceBadge
                    state={entry.stale && entry.state === "passing" ? "stale" : entry.state}
                  />
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
      <CopyIdMenu id={objective.objective_id} what={objective.title} />
    </li>
  );
}

function IntentRow({
  intent,
  view,
  names,
}: {
  intent: RadarView["active_intents"][number];
  view: RadarView;
  names: Names;
}) {
  const owner = names.intentOwner(intent.intent_id);
  const stitches = view.open_collisions.filter(
    (collision) =>
      collision.left_intent_id === intent.intent_id ||
      collision.right_intent_id === intent.intent_id,
  ).length;
  const name = names.intent(intent.intent_id);
  return (
    <li
      className={stitches > 0 ? "row row-bolted" : "row"}
      id={`intent-${intent.intent_id}`}
      tabIndex={-1}
    >
      <Avatar human={names.humanRecord(owner)} />
      <div className="row-main">
        <p className="row-title">{name}</p>
        <p className="row-meta">
          {names.human(owner)} · {names.objective(intent.objective_id)}
          {intent.revision > 1 && ` · revision ${intent.revision}`}
        </p>
        {(intent.repository_id || intent.branch_ref) && (
          <p className="row-meta">
            {intent.repository_id && (
              <code className="mono">{names.repository(intent.repository_id)}</code>
            )}
            {intent.branch_ref && (
              <>
                {" "}
                on <code className="mono">{intent.branch_ref}</code>
              </>
            )}
          </p>
        )}
      </div>
      {stitches > 0 && (
        <span className="bolts">
          <span className="bolt-dot" aria-hidden="true" />
          <span className="bolt-dot" aria-hidden="true" />
          {stitches === 1 ? "1 open stitch" : `${stitches} open stitches`}
        </span>
      )}
      <CopyIdMenu id={intent.intent_id} what={name} />
    </li>
  );
}

function CollisionRow({ collision, names }: { collision: CollisionAssessment; names: Names }) {
  const [first, second, third, ...rest] = collision.matched_subjects;
  const shown = [first, second, third].filter((subject): subject is string => Boolean(subject));
  return (
    <li className="row row-stitch" id={`collision-${collision.collision_id}`} tabIndex={-1}>
      <div className="row-main">
        <p className="row-meta">
          <CollisionBadge state={collision.state} /> {RULE_LABELS[collision.rule_id]} ·{" "}
          {SEVERITY[collision.severity]} · {Math.round(collision.confidence * 100)}% confidence
        </p>
        <div className="stitch-pair">
          <StitchEnd intentId={collision.left_intent_id} names={names} />
          <span className="stitch-line" aria-hidden="true" />
          <span className="sr-only">collides with</span>
          <StitchEnd intentId={collision.right_intent_id} names={names} />
        </div>
        <p className="row-meta">
          On{" "}
          {shown.map((subject, index) => (
            <span key={subject}>
              {index > 0 && ", "}
              <code className="mono">{subject}</code>
            </span>
          ))}
          {rest.length > 0 && ` and ${rest.length} more`}
        </p>
        {collision.latest_combination_outcome && (
          <p className="row-meta">
            <OutcomeBadge outcome={collision.latest_combination_outcome} />
          </p>
        )}
      </div>
      <CopyIdMenu id={collision.collision_id} what={names.collisionName(collision)} />
    </li>
  );
}

function StitchEnd({ intentId, names }: { intentId: UUID; names: Names }) {
  const owner = names.intentOwner(intentId);
  return (
    <span className="stitch-end">
      <Avatar human={names.humanRecord(owner)} size={22} />
      <span>
        <strong>{names.intent(intentId)}</strong>
        <span className="muted"> {names.human(owner)}</span>
      </span>
    </span>
  );
}

function StatementList({ items, empty }: { items: string[]; empty: string }) {
  if (items.length === 0) {
    return <p className="muted">{empty}</p>;
  }
  return (
    <ul className="statements">
      {items.map((item) => (
        <li key={item}>{item}</li>
      ))}
    </ul>
  );
}
