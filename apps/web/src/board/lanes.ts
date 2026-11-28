import type { BoardCard, BoardView, Lane } from "../api/types";
import type { Label } from "../labels";

// The Intent board ("The Slab") shows lanes the backend derives. The Lab
// never decides a lane, never moves a card by hand, and never counts people
// (web-lab-board requirements 1 to 4). These functions are pure so the
// refusal and the "It's alive!" rules can be tested without a browser.

export const LANES: Record<Lane, Label & { hint: string }> = {
  assembling: {
    themed: "Assembling",
    plain: "In progress",
    hint: "No head commit or observed analysis yet",
  },
  stitching: {
    themed: "Stitching",
    plain: "Needs a decision",
    hint: "An open stitch waits for a human decision",
  },
  charging: {
    themed: "Charging",
    plain: "Gathering proof",
    hint: "Acceptance criteria lack passing, current evidence",
  },
  alive: { themed: "Alive", plain: "Ready for review", hint: "The proof is there" },
  buried: { themed: "Buried", plain: "Released", hint: "Released recently" },
};

/** Display order, as the board-view contract fixes it. */
export const LANE_ORDER: Lane[] = ["assembling", "stitching", "charging", "alive", "buried"];

/**
 * Why a card cannot be dragged, and what would move it. Lanes follow Conn
 * records only, so every drop is refused.
 */
export function refusal(card: BoardCard, lane: Lane, target?: Lane): string {
  const here = LANES[lane].plain;
  const prefix =
    target && target !== lane
      ? `Cards cannot be moved by hand, so this one stays in ${here}.`
      : `This card stays in ${here} until Conn records change.`;
  const next =
    card.reason.next_step ??
    (lane === "alive"
      ? "It is ready for review; open it to read the proof."
      : lane === "buried"
        ? "It was released; it leaves the board after the window."
        : "Lanes follow Conn records only.");
  return `${prefix} ${next}`;
}

/** The lane each Intent is in. */
export function lanesById(view: BoardView | undefined): Map<string, Lane> {
  const lanes = new Map<string, Lane>();
  for (const lane of view?.lanes ?? []) {
    for (const card of lane.cards) lanes.set(card.intent_id, lane.lane);
  }
  return lanes;
}

const revisionKey = (card: BoardCard) => `${card.intent_id}@${card.intent_revision}`;

/**
 * The cards to celebrate with "It's alive!" after a refetch: cards that a
 * streamed event moved into Alive while the board was open, once per Intent
 * revision. On first load (no previous view) nothing plays, and every card
 * already Alive is remembered as celebrated. `celebrated` lives in memory
 * only and is updated in place.
 */
export function newlyAlive(
  previous: BoardView | undefined,
  next: BoardView,
  celebrated: Set<string>,
): BoardCard[] {
  const alive = next.lanes.find((lane) => lane.lane === "alive")?.cards ?? [];
  if (!previous) {
    for (const card of alive) celebrated.add(revisionKey(card));
    return [];
  }
  const before = lanesById(previous);
  const result: BoardCard[] = [];
  for (const card of alive) {
    const key = revisionKey(card);
    if (celebrated.has(key)) continue;
    celebrated.add(key);
    if (before.get(card.intent_id) !== "alive") result.push(card);
  }
  return result;
}

/** Intents whose lane changed between two views, for the move transition. */
export function movedCards(previous: BoardView | undefined, next: BoardView): Set<string> {
  const moved = new Set<string>();
  if (!previous) return moved;
  const before = lanesById(previous);
  for (const [intentId, lane] of lanesById(next)) {
    const was = before.get(intentId);
    if (was && was !== lane) moved.add(intentId);
  }
  return moved;
}
