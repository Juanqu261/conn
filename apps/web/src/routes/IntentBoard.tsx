import {
  DndContext,
  type DragEndEvent,
  type DragStartEvent,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { boardQuery } from "../api/queries";
import type { BoardCard, BoardView, Lane } from "../api/types";
import { splitListeners } from "../board/drag";
import { type BoardSearch, type Grouping, swimlanes } from "../board/filters";
import { LANE_ORDER, LANES, movedCards, newlyAlive, refusal } from "../board/lanes";
import { GripIcon, StitchIcon } from "../components/icons";
import { useDocumentTitle } from "../components/title";
import { Avatar, EmptyState, RelativeTime, ThemedLabel } from "../components/ui";
import { LABELS } from "../labels";
import type { Names } from "../names/directory";
import { useProjectData } from "../project";
import { QueryState, useProjectId, useProjectSlug, useViewer } from "./common";

const FLASH_MS = 3000;
const MOVE_MS = 600;

/**
 * The Slab: every current Intent in the one lane Conn derives for it, with
 * the rule and facts behind it (web-lab-board requirements 1 to 4). Cards
 * can be picked up, but every drop is refused with what would move them.
 */
export function IntentBoard() {
  const projectId = useProjectId();
  const search = useSearch({ from: "/p/$project/board" });
  const { names, radar } = useProjectData(projectId);
  const { me } = useViewer(projectId);
  const board = useQuery(boardQuery(projectId));
  useDocumentTitle(LABELS.board.themed);

  const previous = useRef<BoardView | undefined>(undefined);
  const celebrated = useRef(new Set<string>());
  const [flashing, setFlashing] = useState<Set<string>>(new Set());
  const [moved, setMoved] = useState<Set<string>>(new Set());
  const [announcement, setAnnouncement] = useState("");
  const [refused, setRefused] = useState<string>("");

  useEffect(() => {
    const view = board.data;
    if (!view) return;
    const alive = newlyAlive(previous.current, view, celebrated.current);
    const changed = movedCards(previous.current, view);
    previous.current = view;
    const timers: number[] = [];
    if (changed.size > 0) {
      setMoved(changed);
      timers.push(window.setTimeout(() => setMoved(new Set()), MOVE_MS));
    }
    if (alive.length > 0) {
      setFlashing(new Set(alive.map((card) => card.intent_id)));
      setAnnouncement(alive.map((card) => `Ready for review: ${card.summary_line}`).join(". "));
      timers.push(window.setTimeout(() => setFlashing(new Set()), FLASH_MS));
    }
    return () => {
      for (const timer of timers) window.clearTimeout(timer);
    };
  }, [board.data]);

  return (
    <div className="page page-wide">
      <header className="page-header">
        <ThemedLabel label={LABELS.board} as="h1" className="page-title" />
        {board.data && (
          <p className="as-of">
            As of event {board.data.as_of_event.toLocaleString()} · released Intents stay for{" "}
            {board.data.window_days} days
          </p>
        )}
      </header>
      <p className="page-lede">
        Lanes follow Conn records only. Nobody moves a card: publishing, deciding a stitch,
        analysing a revision, or attaching evidence does.
      </p>

      <Filters
        search={search}
        names={names}
        objectives={radar.data?.objectives ?? []}
        repositories={radar.data?.repositories ?? []}
      />

      <div className="sr-only" aria-live="polite">
        {announcement}
      </div>
      <div className="sr-only" aria-live="assertive">
        {refused}
      </div>
      {refused && (
        <div className="notice notice-warning board-refusal" aria-hidden="true">
          <p>{refused}</p>
          <button
            type="button"
            className="button button-secondary button-small"
            onClick={() => setRefused("")}
          >
            Dismiss
          </button>
        </div>
      )}

      <QueryState query={board} what="the board">
        {() =>
          board.data && (
            <Board
              view={board.data}
              search={search}
              me={me}
              names={names}
              flashing={flashing}
              moved={moved}
              onRefuse={setRefused}
            />
          )
        }
      </QueryState>
    </div>
  );
}

function Filters({
  search,
  names,
  objectives,
  repositories,
}: {
  search: BoardSearch;
  names: Names;
  objectives: Array<{ objective_id: string; title: string }>;
  repositories: Array<{ repository_id: string }>;
}) {
  const navigate = useNavigate({ from: "/p/$project/board" });
  const set = (patch: Partial<BoardSearch>) =>
    void navigate({ search: (current) => ({ ...current, ...patch }), replace: true });
  return (
    <form
      className="toolbar"
      aria-label="Board filters"
      onSubmit={(event) => event.preventDefault()}
    >
      <button
        type="button"
        className="chip"
        aria-pressed={Boolean(search.mine)}
        onClick={() => set({ mine: search.mine ? undefined : true })}
      >
        Mine
      </button>
      <label className="field">
        <span>Objective</span>
        <select
          value={search.objective ?? ""}
          onChange={(event) => set({ objective: event.target.value || undefined })}
        >
          <option value="">All</option>
          {objectives.map((objective) => (
            <option key={objective.objective_id} value={objective.objective_id}>
              {objective.title}
            </option>
          ))}
        </select>
      </label>
      <label className="field">
        <span>Repository</span>
        <select
          value={search.repository ?? ""}
          onChange={(event) => set({ repository: event.target.value || undefined })}
        >
          <option value="">All</option>
          {repositories.map((repository) => (
            <option key={repository.repository_id} value={repository.repository_id}>
              {names.repository(repository.repository_id)}
            </option>
          ))}
        </select>
      </label>
      <label className="field">
        <span>Lane</span>
        <select
          value={search.lane ?? ""}
          onChange={(event) => set({ lane: (event.target.value || undefined) as Lane | undefined })}
        >
          <option value="">All</option>
          {LANE_ORDER.map((lane) => (
            <option key={lane} value={lane}>
              {LANES[lane].plain}
            </option>
          ))}
        </select>
      </label>
      <label className="field">
        <span>Swimlanes</span>
        <select
          value={search.group ?? "none"}
          onChange={(event) =>
            set({
              group: event.target.value === "none" ? undefined : (event.target.value as Grouping),
            })
          }
        >
          <option value="none">None</option>
          <option value="objective">By Objective</option>
          <option value="repository">By repository</option>
        </select>
      </label>
    </form>
  );
}

function Board({
  view,
  search,
  me,
  names,
  flashing,
  moved,
  onRefuse,
}: {
  view: BoardView;
  search: BoardSearch;
  me: string | undefined;
  names: Names;
  flashing: Set<string>;
  moved: Set<string>;
  onRefuse(message: string): void;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );
  const cards = new Map<string, { card: BoardCard; lane: Lane }>();
  for (const lane of view.lanes)
    for (const card of lane.cards) cards.set(card.intent_id, { card, lane: lane.lane });
  const describe = (id: unknown) => {
    const found = cards.get(String(id));
    return found ? found.card.summary_line : "this card";
  };

  const onDragStart = (event: DragStartEvent) => {
    const found = cards.get(String(event.active.id));
    if (found) onRefuse(refusal(found.card, found.lane));
  };
  // Every drop is refused: the card stays, and the member learns what
  // would move it (requirement 3.2).
  const onDragEnd = (event: DragEndEvent) => {
    const found = cards.get(String(event.active.id));
    const target = event.over?.data.current?.lane as Lane | undefined;
    if (found) onRefuse(refusal(found.card, found.lane, target));
  };

  const groups = swimlanes(view, search, me);
  const visible = groups.some((group) => group.lanes.some((lane) => lane.cards.length > 0));
  return (
    <DndContext
      sensors={sensors}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      accessibility={{
        announcements: {
          onDragStart: ({ active }) =>
            `Picked up ${describe(active.id)}. Cards cannot be moved by hand.`,
          onDragOver: () => "Lanes follow Conn records only.",
          onDragEnd: ({ active }) => `${describe(active.id)} stays in its lane.`,
          onDragCancel: ({ active }) => `${describe(active.id)} stays in its lane.`,
        },
        screenReaderInstructions: {
          draggable:
            "Cards on this board cannot be moved by hand. Picking one up tells you what would move it.",
        },
      }}
    >
      {view.omitted > 0 && (
        <p className="notice notice-warning">
          The board lists at most 500 cards per lane; {view.omitted} more are counted in the lane
          totals but not shown.
        </p>
      )}
      {!visible && (
        <EmptyState title="No Intent matches these filters">
          Clear a filter, or publish an Intent from Kiro or the CLI to see it here.
        </EmptyState>
      )}
      {groups.map((group) => (
        <section
          key={group.key}
          className="swimlane"
          aria-label={
            search.group === "objective"
              ? `Objective ${names.objective(group.id)}`
              : search.group === "repository"
                ? `Repository ${group.id ? names.repository(group.id) : "not connected"}`
                : "All Intents"
          }
        >
          {search.group && (
            <h2 className="swimlane-title">
              {search.group === "objective"
                ? names.objective(group.id)
                : group.id
                  ? names.repository(group.id)
                  : "No repository connected"}
            </h2>
          )}
          <div className="slab">
            {group.lanes.map((lane) => (
              <LaneColumn
                key={lane.lane}
                lane={lane.lane}
                count={lane.count}
                cards={lane.cards}
                names={names}
                flashing={flashing}
                moved={moved}
                groupKey={group.key}
              />
            ))}
          </div>
        </section>
      ))}
    </DndContext>
  );
}

function LaneColumn({
  lane,
  count,
  cards,
  names,
  flashing,
  moved,
  groupKey,
}: {
  lane: Lane;
  count: number;
  cards: BoardCard[];
  names: Names;
  flashing: Set<string>;
  moved: Set<string>;
  groupKey: string;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `${groupKey}:${lane}`, data: { lane } });
  const headingId = `lane-${groupKey}-${lane}`;
  return (
    <section
      className={`lane lane-${lane}${isOver ? " lane-over" : ""}`}
      aria-labelledby={headingId}
      ref={setNodeRef}
    >
      <header className="lane-header">
        <h3 id={headingId} className="lane-title">
          <ThemedLabel label={LANES[lane]} />
          <span className="lane-count">
            {count}
            <span className="sr-only"> Intents</span>
          </span>
        </h3>
        <p className="lane-hint">{LANES[lane].hint}</p>
      </header>
      {cards.length === 0 ? (
        <p className="lane-empty">Empty</p>
      ) : (
        <ol className="lane-cards">
          {cards.map((card) => (
            <li key={card.intent_id}>
              <IntentCard
                card={card}
                lane={lane}
                names={names}
                flashing={flashing.has(card.intent_id)}
                moved={moved.has(card.intent_id)}
              />
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function IntentCard({
  card,
  lane,
  names,
  flashing,
  moved,
}: {
  card: BoardCard;
  lane: Lane;
  names: Names;
  flashing: boolean;
  moved: boolean;
}) {
  const slug = useProjectSlug();
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, isDragging } =
    useDraggable({
      id: card.intent_id,
      data: { lane },
    });
  const owner = names.humanRecord(card.owner_human_id);
  const stitches = card.open_collision_ids.length;
  const style = transform
    ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` }
    : undefined;
  const classes = ["slab-card", `slab-card-${lane}`];
  if (stitches > 0) classes.push("row-bolted");
  if (flashing) classes.push("its-alive");
  if (moved) classes.push("card-moved");
  if (isDragging) classes.push("card-dragging");
  return (
    <article
      ref={setNodeRef}
      className={classes.join(" ")}
      style={style}
      aria-labelledby={`card-${card.intent_id}`}
      onPointerDown={splitListeners(listeners).onPointerDown}
    >
      <header className="slab-card-header">
        <Link
          id={`card-${card.intent_id}`}
          to="/p/$project/intents/$intent"
          params={{ project: slug, intent: card.intent_id }}
          className="slab-card-title"
        >
          {card.summary_line}
        </Link>
        <button
          type="button"
          ref={setActivatorNodeRef}
          className="icon-button grip"
          aria-label={`Try to move ${card.summary_line}`}
          {...attributes}
          onKeyDown={splitListeners(listeners).onKeyDown}
        >
          <GripIcon size={16} />
        </button>
      </header>
      <p className="slab-card-owner">
        <Avatar human={owner} size={20} />
        <span>{names.human(card.owner_human_id)}</span>
        <span className="muted">· {names.objective(card.objective_id)}</span>
      </p>
      {(card.repository_id || card.branch_ref) && (
        <p className="row-meta mono">
          {card.repository_id ? names.repository(card.repository_id) : "No repository"}
          {card.branch_ref ? ` · ${card.branch_ref}` : ""}
        </p>
      )}
      <div className="slab-card-facts">
        {stitches > 0 && (
          <span className="bolts" title="Open stitches on this revision">
            <StitchIcon size={14} />
            {stitches} open {stitches === 1 ? "stitch" : "stitches"}
          </span>
        )}
        <Jars passing={card.acceptance.passing} required={card.acceptance.required} />
        {card.latest_evidence_at ? (
          <span className={card.evidence_stale ? "evidence-age stale" : "evidence-age"}>
            Latest evidence <RelativeTime value={card.latest_evidence_at} />
            {card.evidence_stale ? " · stale" : ""}
          </span>
        ) : (
          <span className="evidence-age muted">No evidence yet</span>
        )}
      </div>
      <p className="slab-card-reason">{card.reason.facts_markdown}</p>
      {flashing && <span className="alive-tag">It's alive!</span>}
    </article>
  );
}

/** Evidence progress as specimen jars: passing over required acceptance criteria. */
export function Jars({ passing, required }: { passing: number; required: number }) {
  if (required === 0) {
    return <span className="jars muted">No acceptance criteria</span>;
  }
  const shown = Math.min(required, 8);
  const filled = Math.round((passing / required) * shown);
  return (
    <span className="jars">
      <span className="sr-only">
        {passing} of {required} acceptance criteria passing
      </span>
      <span aria-hidden="true" className="jar-row">
        {Array.from({ length: shown }, (_, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: jars are positional
          <span key={i} className={i < filled ? "jar jar-full" : "jar"} />
        ))}
      </span>
      <span aria-hidden="true">
        {passing}/{required}
      </span>
    </span>
  );
}
