import type { BoardCard, BoardView, Lane, UUID } from "../api/types";
import { LANE_ORDER } from "./lanes";

// Board filters live in the URL so a filtered board can be shared
// (requirement 2.3). Filters narrow what is shown; they never add a
// per-person total or ranking (requirement 2.4).

export type Grouping = "none" | "objective" | "repository";

export interface BoardSearch {
  mine?: boolean | undefined;
  objective?: UUID | undefined;
  repository?: UUID | undefined;
  lane?: Lane | undefined;
  group?: Grouping | undefined;
}

const isLane = (value: unknown): value is Lane =>
  typeof value === "string" && (LANE_ORDER as string[]).includes(value);

/** Parses untrusted URL search parameters into board filters. */
export function parseBoardSearch(search: Record<string, unknown>): BoardSearch {
  const text = (value: unknown) => (typeof value === "string" && value ? value : undefined);
  const group =
    search.group === "objective" || search.group === "repository" ? search.group : undefined;
  return {
    mine: search.mine === true || search.mine === "true" ? true : undefined,
    objective: text(search.objective),
    repository: text(search.repository),
    lane: isLane(search.lane) ? search.lane : undefined,
    group,
  };
}

export function matches(card: BoardCard, search: BoardSearch, me: UUID | undefined): boolean {
  if (search.mine && card.owner_human_id !== me) return false;
  if (search.objective && card.objective_id !== search.objective) return false;
  if (search.repository && card.repository_id !== search.repository) return false;
  return true;
}

export interface Swimlane {
  key: string;
  /** Objective or repository ID; undefined for the ungrouped board or cards without one. */
  id: UUID | undefined;
  lanes: Array<{ lane: Lane; count: number; cards: BoardCard[] }>;
}

/**
 * The visible board: lanes in contract order, filtered, and grouped into
 * swimlanes by Objective or repository. Counts are Project-level lane totals
 * of the visible cards only.
 */
export function swimlanes(view: BoardView, search: BoardSearch, me: UUID | undefined): Swimlane[] {
  const lanes = view.lanes.filter((lane) => !search.lane || lane.lane === search.lane);
  const groupOf = (card: BoardCard): UUID | undefined =>
    search.group === "objective"
      ? card.objective_id
      : search.group === "repository"
        ? card.repository_id
        : undefined;
  const keys: Array<UUID | undefined> = [];
  const seen = new Set<string>();
  for (const lane of lanes) {
    for (const card of lane.cards) {
      if (!matches(card, search, me)) continue;
      const group = groupOf(card);
      const key = group ?? "";
      if (!seen.has(key)) {
        seen.add(key);
        keys.push(group);
      }
    }
  }
  if (keys.length === 0) keys.push(undefined);
  return keys.map((group) => ({
    key: group ?? "none",
    id: group,
    lanes: lanes.map((lane) => {
      const cards = lane.cards.filter(
        (card) => matches(card, search, me) && (!search.group || groupOf(card) === group),
      );
      const filtered = Boolean(
        search.mine || search.objective || search.repository || search.group,
      );
      // Unfiltered, the backend total also counts cards beyond the bound.
      return { lane: lane.lane, count: filtered ? cards.length : lane.count, cards };
    }),
  }));
}
