import {
  DndContext,
  type DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { collisionsInStateQuery } from "../api/queries";
import type { CollisionAssessment, CollisionState, UUID } from "../api/types";
import { splitListeners } from "../board/drag";
import { GripIcon } from "../components/icons";
import { useDocumentTitle } from "../components/title";
import { Avatar, CollisionBadge, OutcomeBadge, ThemedLabel } from "../components/ui";
import { LABELS } from "../labels";
import { type Names, RULE_LABELS } from "../names/directory";
import { useProjectData } from "../project";
import { DecisionDialog } from "../stitches/DecisionDialog";
import { type DecisionTarget, decidable, dropTargets } from "../stitches/decision";
import { QueryState, SEVERITY_LABELS, useProjectId, useProjectSlug } from "./common";

const COLUMNS: Array<{ state: CollisionState; title: string; hint: string }> = [
  { state: "open", title: "Open", hint: "Advisory until a human decides" },
  { state: "under_review", title: "Under review", hint: "Escalated or handled another way" },
  { state: "resolved", title: "Resolved", hint: "A human decision closed it" },
  { state: "dismissed", title: "Dismissed", hint: "A human found no interaction" },
  { state: "stale", title: "Stale", hint: "A newer Intent revision replaced it" },
];

interface Deciding {
  collisionId: UUID;
  target: DecisionTarget | undefined;
}

/**
 * Stitches: collisions by state (requirement 5). Dropping an Open or Under
 * review card on another column, or pressing d on a focused card, opens the
 * decision review dialog; nothing changes until the member confirms.
 * Resolved, Dismissed, and Stale cards never move.
 */
export function CollisionBoard() {
  const projectId = useProjectId();
  const { radar, names } = useProjectData(projectId);
  const stale = useQuery(collisionsInStateQuery(projectId, "stale"));
  const [deciding, setDeciding] = useState<Deciding | undefined>();
  useDocumentTitle(LABELS.stitchBoard.themed);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );

  const onDragEnd = (event: DragEndEvent) => {
    const collision = event.active.data.current?.collision as CollisionAssessment | undefined;
    const target = event.over?.id as CollisionState | undefined;
    if (!collision || !target || target === collision.state) return;
    if ((dropTargets(collision.state) as CollisionState[]).includes(target)) {
      setDeciding({ collisionId: collision.collision_id, target: target as DecisionTarget });
    }
  };

  return (
    <div className="page page-wide">
      <header className="page-header">
        <ThemedLabel label={LABELS.stitchBoard} as="h1" className="page-title" />
        {radar.data && (
          <p className="as-of">As of event {radar.data.as_of_event.toLocaleString()}</p>
        )}
      </header>
      <p className="page-lede">
        Drag an open stitch to a column, or focus a card and press <kbd className="kbd">d</kbd>, to
        review both sides and record a decision. Decided and stale stitches change only through a
        new decision or a new Intent revision.
      </p>
      <QueryState query={radar} what="the stitches">
        {() => {
          const view = radar.data;
          if (!view) return null;
          const byState: Record<CollisionState, CollisionAssessment[]> = {
            open: [],
            under_review: [],
            resolved: [],
            dismissed: [],
            stale: stale.data?.collisions ?? [],
          };
          for (const collision of [...view.open_collisions, ...(view.decided_collisions ?? [])]) {
            byState[collision.state]?.push(collision);
          }
          return (
            <DndContext
              sensors={sensors}
              onDragEnd={onDragEnd}
              accessibility={{
                announcements: {
                  onDragStart: () =>
                    "Picked up a stitch. Drop it on Under review, Resolved, or Dismissed to review a decision.",
                  onDragOver: ({ over }) =>
                    over ? `Over ${String(over.id).replace("_", " ")}.` : "Not over a column.",
                  onDragEnd: ({ over }) =>
                    over
                      ? "Opening the decision review. Nothing has changed yet."
                      : "Dropped outside a column; nothing changed.",
                  onDragCancel: () => "Nothing changed.",
                },
              }}
            >
              <div className="stitch-board">
                {COLUMNS.map((column) => (
                  <Column
                    key={column.state}
                    column={column}
                    collisions={byState[column.state]}
                    names={names}
                    note={
                      column.state === "stale" && stale.data?.truncated
                        ? "Only the 50 most recent stale stitches are shown."
                        : column.state !== "open" &&
                            column.state !== "under_review" &&
                            column.state !== "stale" &&
                            (view.decided_collisions?.length ?? 0) >= 50
                          ? "Only the 50 most recent decided stitches are shown."
                          : undefined
                    }
                    onDecide={(collisionId) => setDeciding({ collisionId, target: undefined })}
                  />
                ))}
              </div>
            </DndContext>
          );
        }}
      </QueryState>
      {deciding && (
        <DecisionDialog
          projectId={projectId}
          collisionId={deciding.collisionId}
          target={deciding.target}
          open
          onOpenChange={(open) => {
            if (!open) setDeciding(undefined);
          }}
        />
      )}
    </div>
  );
}

function Column({
  column,
  collisions,
  names,
  note,
  onDecide,
}: {
  column: { state: CollisionState; title: string; hint: string };
  collisions: CollisionAssessment[];
  names: Names;
  note: string | undefined;
  onDecide(collisionId: UUID): void;
}) {
  const accepts =
    column.state === "under_review" || column.state === "resolved" || column.state === "dismissed";
  const { setNodeRef, isOver } = useDroppable({ id: column.state, disabled: !accepts });
  const headingId = `column-${column.state}`;
  return (
    <section
      ref={setNodeRef}
      className={`lane stitch-column${isOver ? " lane-over" : ""}`}
      aria-labelledby={headingId}
    >
      <header className="lane-header">
        <h2 id={headingId} className="lane-title">
          {column.title}
          <span className="lane-count">
            {collisions.length}
            <span className="sr-only"> stitches</span>
          </span>
        </h2>
        <p className="lane-hint">{column.hint}</p>
      </header>
      {note && <p className="lane-hint">{note}</p>}
      {collisions.length === 0 ? (
        <p className="lane-empty">Empty</p>
      ) : (
        <ol className="lane-cards">
          {collisions.map((collision) => (
            <li key={collision.collision_id}>
              <StitchCard collision={collision} names={names} onDecide={onDecide} />
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function StitchCard({
  collision,
  names,
  onDecide,
}: {
  collision: CollisionAssessment;
  names: Names;
  onDecide(collisionId: UUID): void;
}) {
  const slug = useProjectSlug();
  const movable = decidable(collision.state);
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, isDragging } =
    useDraggable({
      id: collision.collision_id,
      data: { collision },
      disabled: !movable,
    });
  const style = transform
    ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` }
    : undefined;
  const sides = [
    { id: collision.left_intent_id, revision: collision.left_intent_revision },
    { id: collision.right_intent_id, revision: collision.right_intent_revision },
  ];
  return (
    <article
      ref={setNodeRef}
      style={style}
      tabIndex={-1}
      className={`slab-card stitch-card stitch-${collision.state}${isDragging ? " card-dragging" : ""}`}
      aria-labelledby={`stitch-${collision.collision_id}`}
      onPointerDown={movable ? splitListeners(listeners).onPointerDown : undefined}
      onKeyDown={(event) => {
        if (event.key === "d" && movable && !event.metaKey && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          onDecide(collision.collision_id);
        }
      }}
    >
      <header className="slab-card-header">
        <Link
          id={`stitch-${collision.collision_id}`}
          to="/p/$project/stitches/$collision"
          params={{ project: slug, collision: collision.collision_id }}
          className="slab-card-title"
        >
          {RULE_LABELS[collision.rule_id] ?? collision.rule_id}
        </Link>
        {movable && (
          <button
            type="button"
            ref={setActivatorNodeRef}
            className="icon-button grip"
            aria-label={`Drag this stitch to a decision column`}
            {...attributes}
            onKeyDown={splitListeners(listeners).onKeyDown}
          >
            <GripIcon size={16} />
          </button>
        )}
      </header>
      <ul className="stitch-card-sides">
        {sides.map((side) => {
          const owner = names.intentOwner(side.id);
          return (
            <li key={side.id}>
              <Avatar human={names.humanRecord(owner)} size={18} /> {names.human(owner)} ·{" "}
              {names.intent(side.id)} <span className="muted">rev {side.revision}</span>
            </li>
          );
        })}
      </ul>
      <p className="row-meta mono">
        {collision.matched_subjects.slice(0, 3).join(", ")}
        {collision.matched_subjects.length > 3
          ? ` and ${collision.matched_subjects.length - 3} more`
          : ""}
      </p>
      <p className="slab-card-facts">
        <CollisionBadge state={collision.state} />
        <span className="row-meta">
          {SEVERITY_LABELS[collision.severity]} severity · confidence{" "}
          {collision.confidence.toFixed(2)}
        </span>
        {collision.latest_combination_outcome && (
          <OutcomeBadge outcome={collision.latest_combination_outcome} />
        )}
      </p>
      {movable && (
        <button
          type="button"
          className="button button-secondary button-small"
          onClick={() => onDecide(collision.collision_id)}
        >
          Decide… <kbd className="kbd">d</kbd>
        </button>
      )}
    </article>
  );
}
