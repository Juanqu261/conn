import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Dialog } from "radix-ui";
import { useId, useState } from "react";
import { apiPost } from "../api/client";
import {
  blueprintRevisionQuery,
  charterDecisionPath,
  objectiveRevisionQuery,
  projectKey,
  statusQuery,
} from "../api/queries";
import type {
  BlueprintRevisionSummary,
  BoundaryKind,
  CellCharter,
  CellDecision,
  EvidenceCoverage,
  ProjectStatus,
  UUID,
} from "../api/types";
import { BoltIcon, CrossIcon } from "../components/icons";
import { useDocumentTitle } from "../components/title";
import { Avatar, EmptyState, EvidenceBadge, ThemedLabel } from "../components/ui";
import { LABELS } from "../labels";
import type { Names } from "../names/directory";
import { useProjectData } from "../project";
import { MarkdownText, QueryState, useProjectId, useViewer } from "./common";

const DECISIONS: Record<CellDecision, string> = {
  pending: "Pending",
  accept: "Accepted",
  reject: "Rejected",
};

const BOUNDARIES: Array<{ kind: BoundaryKind; title: string }> = [
  { kind: "responsibility", title: "Responsibilities" },
  { kind: "exclusion", title: "Exclusions" },
  { kind: "deliverable", title: "Deliverables" },
  { kind: "evidence_obligation", title: "Evidence obligations" },
];

/**
 * Objectives with their key points and evidence coverage, and Blueprints
 * with their Cells (requirement 9). The signed-in Human may accept or reject
 * only their own pending Charter; the backend refuses anything else.
 */
export function Blueprints() {
  const projectId = useProjectId();
  const status = useQuery(statusQuery(projectId));
  const { radar, names } = useProjectData(projectId);
  const { me } = useViewer(projectId);
  useDocumentTitle(LABELS.blueprints.themed);
  return (
    <div className="page">
      <header className="page-header">
        <ThemedLabel label={LABELS.blueprints} as="h1" className="page-title" />
      </header>
      <QueryState query={status} what="the Objectives and Blueprints">
        {() => {
          const data = status.data;
          if (!data) return null;
          if (data.objectives.length === 0) {
            return (
              <EmptyState title="No Objectives yet">
                Create or import an Objective from the CLI or Kiro.
              </EmptyState>
            );
          }
          return data.objectives.map((objective) => (
            <ObjectiveSection
              key={objective.objective_id}
              projectId={projectId}
              objectiveId={objective.objective_id}
              revision={objective.current_revision}
              coverage={radar.data?.evidence_coverage ?? []}
              status={data}
              names={names}
              me={me}
            />
          ));
        }}
      </QueryState>
    </div>
  );
}

function ObjectiveSection({
  projectId,
  objectiveId,
  revision,
  coverage,
  status,
  names,
  me,
}: {
  projectId: UUID;
  objectiveId: UUID;
  revision: number;
  coverage: EvidenceCoverage[];
  status: ProjectStatus;
  names: Names;
  me: UUID | undefined;
}) {
  const objective = useQuery(objectiveRevisionQuery(projectId, objectiveId, revision));
  const blueprints = status.blueprints.flatMap((blueprint) =>
    blueprint.revisions
      .filter((item) => item.objective_id === objectiveId)
      .map((item) => ({
        blueprintId: blueprint.blueprint_id,
        summary: item,
        all: blueprint.revisions,
      })),
  );
  return (
    <section className="section" aria-labelledby={`objective-${objectiveId}`}>
      <QueryState query={objective} what="the Objective">
        {() => {
          const data = objective.data;
          if (!data) return null;
          const points = data.key_points ?? [];
          return (
            <>
              <h2 id={`objective-${objectiveId}`} className="section-title">
                {data.title} <span className="muted">revision {data.revision}</span>
              </h2>
              <p className="row-meta">Accountable: {names.human(data.accountable_human_id)}</p>
              <MarkdownText text={data.overview_markdown} />
              {points.length > 0 && (
                <ul className="checklist">
                  {points.map((point) => {
                    const covered = coverage.find(
                      (item) =>
                        item.objective_id === objectiveId &&
                        item.objective_revision === revision &&
                        item.acceptance_key === point.key,
                    );
                    return (
                      <li key={point.key} className="checklist-item">
                        {point.kind === "acceptance_criterion" ? (
                          <EvidenceBadge
                            state={covered?.stale ? "stale" : (covered?.state ?? "missing")}
                          />
                        ) : (
                          <span className="badge badge-neutral">
                            {point.kind.replace("_", " ")}
                          </span>
                        )}
                        <span>
                          <code>{point.key}</code> {point.statement}
                          {point.kind === "acceptance_criterion" && covered?.provenance && (
                            <span className="row-meta">
                              {" "}
                              · {covered.provenance === "observed_run" ? "observed" : "reported"}
                            </span>
                          )}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              )}
            </>
          );
        }}
      </QueryState>
      {blueprints.map(({ blueprintId, summary, all }) => (
        <BlueprintRevision
          key={`${blueprintId}-${summary.revision}`}
          projectId={projectId}
          blueprintId={blueprintId}
          summary={summary}
          all={all}
          names={names}
          me={me}
        />
      ))}
    </section>
  );
}

function BlueprintRevision({
  projectId,
  blueprintId,
  summary,
  all,
  names,
  me,
}: {
  projectId: UUID;
  blueprintId: UUID;
  summary: BlueprintRevisionSummary;
  all: BlueprintRevisionSummary[];
  names: Names;
  me: UUID | undefined;
}) {
  const [deciding, setDeciding] = useState<
    { cellId: UUID; outcome: "accept" | "reject" } | undefined
  >();
  const active = all.find((item) => item.status === "active" && item.revision !== summary.revision);
  return (
    <div className="blueprint">
      <h3 className="subsection-title">
        Blueprint revision {summary.revision} ·{" "}
        <span
          className={`badge badge-${summary.status === "active" ? "alive" : summary.status === "rejected" ? "failing" : "review"}`}
        >
          {summary.status}
        </span>
      </h3>
      <ul className="list">
        {summary.cells.map((cell) => {
          const own =
            cell.accountable_human_id === me &&
            cell.decision === "pending" &&
            summary.status === "proposed";
          return (
            <li key={cell.cell_id} className="row">
              <div className="row-main">
                <span className="row-title">{cell.title}</span>
                <span
                  className={`badge badge-${cell.decision === "accept" ? "alive" : cell.decision === "reject" ? "failing" : "neutral"}`}
                >
                  {DECISIONS[cell.decision]}
                </span>
              </div>
              <p className="row-meta">
                <Avatar human={names.humanRecord(cell.accountable_human_id)} size={18} />{" "}
                {names.human(cell.accountable_human_id)}
              </p>
              {own && (
                <div className="row-actions">
                  <button
                    type="button"
                    className="button button-primary button-small"
                    onClick={() => setDeciding({ cellId: cell.cell_id, outcome: "accept" })}
                  >
                    Accept…
                  </button>
                  <button
                    type="button"
                    className="button button-secondary button-small"
                    onClick={() => setDeciding({ cellId: cell.cell_id, outcome: "reject" })}
                  >
                    Reject…
                  </button>
                </div>
              )}
            </li>
          );
        })}
      </ul>
      {deciding && (
        <CharterDialog
          projectId={projectId}
          blueprintId={blueprintId}
          summary={summary}
          replaces={active?.revision}
          cellId={deciding.cellId}
          outcome={deciding.outcome}
          names={names}
          onClose={() => setDeciding(undefined)}
        />
      )}
    </div>
  );
}

/** The Charter review dialog: the immutable snapshot, then an explicit outcome. */
function CharterDialog({
  projectId,
  blueprintId,
  summary,
  replaces,
  cellId,
  outcome,
  names,
  onClose,
}: {
  projectId: UUID;
  blueprintId: UUID;
  summary: BlueprintRevisionSummary;
  replaces: number | undefined;
  cellId: UUID;
  outcome: "accept" | "reject";
  names: Names;
  onClose(): void;
}) {
  const queryClient = useQueryClient();
  const revision = useQuery(blueprintRevisionQuery(projectId, blueprintId, summary.revision));
  const [rationale, setRationale] = useState("");
  const ids = useId();
  const submit = useMutation({
    mutationFn: () =>
      apiPost(
        charterDecisionPath(projectId, blueprintId, summary.revision, cellId, outcome),
        outcome === "reject" ? { rationale: rationale.trim() } : undefined,
      ),
    onSettled: () => {
      for (const kind of ["status", "plans", "radar"] as const) {
        void queryClient.invalidateQueries({ queryKey: projectKey(kind, projectId) });
      }
    },
  });
  const charter = revision.data?.blueprint_revision.cell_charters.find(
    (cell) => cell.cell_id === cellId,
  );
  const others = summary.cells.filter((cell) => cell.cell_id !== cellId);
  const othersAccepted = others.every((cell) => cell.decision === "accept");
  const consequence =
    outcome === "reject"
      ? `Rejecting makes Blueprint revision ${summary.revision} rejected. Settling it takes a new Blueprint revision.`
      : `If you accept and every other Cell is accepted, Blueprint revision ${summary.revision} becomes active${replaces ? ` and replaces revision ${replaces}` : ""}.${othersAccepted ? " Every other Cell is already accepted, so your acceptance activates it." : ""}`;

  return (
    <Dialog.Root open onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog dialog-wide" aria-describedby={`${ids}-desc`}>
          <header className="dialog-header">
            <Dialog.Title className="dialog-title">
              {outcome === "accept" ? "Accept your Charter" : "Reject your Charter"}
            </Dialog.Title>
            <Dialog.Close className="icon-button" aria-label="Close without deciding">
              <CrossIcon />
            </Dialog.Close>
          </header>
          <Dialog.Description id={`${ids}-desc`} className="dialog-description">
            Read exactly what you commit to. This snapshot of Blueprint revision {summary.revision}{" "}
            never changes.
          </Dialog.Description>
          {submit.isSuccess ? (
            <div className="notice notice-alive" role="status">
              <p>
                {outcome === "accept"
                  ? "Your acceptance is recorded."
                  : "Your rejection is recorded."}
              </p>
              <Dialog.Close className="button button-secondary">Close</Dialog.Close>
            </div>
          ) : (
            <QueryState query={revision} what="the Charter">
              {() =>
                charter ? (
                  <form
                    className="decision-form"
                    onSubmit={(event) => {
                      event.preventDefault();
                      if (outcome === "accept" || rationale.trim()) submit.mutate();
                    }}
                  >
                    <CharterSnapshot
                      charter={charter}
                      cells={revision.data?.blueprint_revision.cell_charters ?? []}
                    />
                    <h3 className="subsection-title">Other Cells</h3>
                    <ul className="statements">
                      {others.map((cell) => (
                        <li key={cell.cell_id}>
                          {cell.title} · {names.human(cell.accountable_human_id)} ·{" "}
                          {DECISIONS[cell.decision]}
                        </li>
                      ))}
                      {others.length === 0 && <li>This Blueprint revision has no other Cells.</li>}
                    </ul>
                    <p className="notice notice-warning">{consequence}</p>
                    {outcome === "reject" && (
                      <label className="field field-block">
                        <span>Rationale (required)</span>
                        <textarea
                          required
                          rows={3}
                          maxLength={4000}
                          value={rationale}
                          onChange={(event) => setRationale(event.target.value)}
                        />
                      </label>
                    )}
                    {submit.isError && (
                      <p className="notice notice-failing" role="alert">
                        {submit.error.message}
                      </p>
                    )}
                    <footer className="dialog-footer">
                      <Dialog.Close className="button button-secondary" type="button">
                        Cancel
                      </Dialog.Close>
                      <button
                        type="submit"
                        className="button button-primary"
                        disabled={submit.isPending || (outcome === "reject" && !rationale.trim())}
                      >
                        <BoltIcon size={16} />{" "}
                        {outcome === "accept" ? "Accept Charter" : "Reject Charter"}
                      </button>
                    </footer>
                  </form>
                ) : (
                  <p className="notice notice-failing">
                    This Cell is not in the Blueprint revision.
                  </p>
                )
              }
            </QueryState>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function CharterSnapshot({ charter, cells }: { charter: CellCharter; cells: CellCharter[] }) {
  const cellName = (id: UUID) => cells.find((cell) => cell.cell_id === id)?.title ?? "another Cell";
  return (
    <div className="charter">
      <h3 className="subsection-title">{charter.title}</h3>
      <MarkdownText text={charter.overview_markdown} />
      {BOUNDARIES.map(({ kind, title }) => {
        const points = charter.boundary_points?.filter((point) => point.kind === kind) ?? [];
        return points.length === 0 ? null : (
          <div key={kind}>
            <h4 className="charter-heading">{title}</h4>
            <ul className="statements">
              {points.map((point) => (
                <li key={point.key}>
                  <code>{point.key}</code> {point.statement}
                </li>
              ))}
            </ul>
          </div>
        );
      })}
      {charter.dependencies && charter.dependencies.length > 0 && (
        <div>
          <h4 className="charter-heading">Dependencies</h4>
          <ul className="statements">
            {charter.dependencies.map((dependency) => (
              <li key={dependency.cell_id}>
                {cellName(dependency.cell_id)}: {dependency.summary}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
