import { describe, expect, it } from "vitest";
import type { BoardCard, BoardView, Lane } from "../src/api/types";
import { LANE_ORDER, newlyAlive, refusal } from "../src/board/lanes";

// Protects web-lab-board requirements 3 and 4: an Intent card never moves
// by hand and says what would move it; "It's alive!" plays once per Intent
// revision, only for a move into Alive seen while the board is open, and
// never on page load.

const card = (id: string, revision: number, nextStep?: string): BoardCard => ({
  intent_id: id,
  intent_revision: revision,
  status: "active",
  summary_line: `Intent ${id}`,
  objective_id: "019a8f00-0002-7000-8000-000000000001",
  objective_revision: 1,
  open_collision_ids: [],
  acceptance: { required: 2, passing: 1, missing_keys: ["fr-004"] },
  evidence_stale: false,
  reason: {
    rule: "acceptance_incomplete",
    facts_markdown: "1 of 2 acceptance criteria passing; missing `fr-004`.",
    ...(nextStep ? { next_step: nextStep } : {}),
  },
});

const view = (placement: Partial<Record<Lane, BoardCard[]>>): BoardView => ({
  board_version: 1,
  project_id: "019a8f00-0001-7000-8000-000000000001",
  as_of_event: 1,
  window_days: 14,
  omitted: 0,
  lanes: LANE_ORDER.map((lane) => ({
    lane,
    count: placement[lane]?.length ?? 0,
    cards: placement[lane] ?? [],
  })),
});

describe("Intent cards refuse to move", () => {
  it("keeps the card in its lane and names what would move it", () => {
    const text = refusal(
      card(
        "a",
        1,
        "Attaching passing evidence at the head commit for `fr-004` moves this to Alive.",
      ),
      "charging",
      "alive",
    );
    expect(text).toContain("cannot be moved by hand");
    expect(text).toContain("Gathering proof");
    expect(text).toContain("`fr-004`");
  });

  it("still explains itself when the backend gives no next step", () => {
    expect(refusal(card("a", 1), "alive", "buried")).toContain("ready for review");
  });
});

describe("It's alive!", () => {
  it("never plays on page load, even for cards already Alive", () => {
    const celebrated = new Set<string>();
    expect(newlyAlive(undefined, view({ alive: [card("a", 1)] }), celebrated)).toEqual([]);
    // The same card, still Alive after a refetch, does not play either.
    expect(
      newlyAlive(view({ alive: [card("a", 1)] }), view({ alive: [card("a", 1)] }), celebrated),
    ).toEqual([]);
  });

  it("plays once when a streamed change moves a card into Alive", () => {
    const celebrated = new Set<string>();
    const before = view({ charging: [card("a", 1)] });
    newlyAlive(undefined, before, celebrated);
    const after = view({ alive: [card("a", 1)] });
    expect(newlyAlive(before, after, celebrated).map((c) => c.intent_id)).toEqual(["a"]);
    expect(newlyAlive(after, after, celebrated)).toEqual([]);
  });

  it("plays at most once per Intent revision", () => {
    const celebrated = new Set<string>();
    const charging = view({ charging: [card("a", 1)] });
    const alive = view({ alive: [card("a", 1)] });
    newlyAlive(undefined, charging, celebrated);
    expect(newlyAlive(charging, alive, celebrated)).toHaveLength(1);
    // Leaving Alive and returning at the same revision stays quiet.
    expect(newlyAlive(alive, charging, celebrated)).toEqual([]);
    expect(newlyAlive(charging, alive, celebrated)).toEqual([]);
    // A new revision earns its own moment.
    const revised = view({ alive: [card("a", 2)] });
    expect(newlyAlive(view({ charging: [card("a", 2)] }), revised, celebrated)).toHaveLength(1);
  });
});
