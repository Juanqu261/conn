import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { EventEnvelope } from "../src/api/types";
import { describeEvent, familyOf, plainText } from "../src/feed/sentences";
import { buildNames, FALLBACK } from "../src/names/directory";
import { invalidationFor } from "../src/stream/invalidation";

// Protects web-lab-foundation requirements 5 and 7.4: every committed event
// type reads as one human sentence that names records and never shows a
// raw identifier, including when a name cannot be resolved.

const contracts = join(import.meta.dirname, "..", "..", "..", "contracts", "v1");
const readJson = (path: string): unknown => JSON.parse(readFileSync(path, "utf8"));
const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

// Every event type the event-envelope contract defines, read from the
// schema so a new type without a sentence fails here.
const schema = readJson(join(contracts, "event-envelope.schema.json")) as {
  allOf: Array<{ if: { properties: { event_type: { const?: string; enum?: string[] } } } }>;
};
const eventTypes = schema.allOf.flatMap((rule) => {
  const type = rule.if.properties.event_type;
  return type.const ? [type.const] : (type.enum ?? []);
});

const { cases } = readJson(join(contracts, "fixtures", "cases.json")) as {
  cases: Array<{ name: string; schema: string; valid: boolean; document: unknown }>;
};
const fixtureEvents = cases
  .filter((c) => c.valid && c.schema === "urn:conn:schema:v1:event-envelope")
  .map((c) => [c.name, c.document as EventEnvelope] as const);

const ids = {
  project: "019a8f00-0001-7000-8000-000000000001",
  alice: "019a8f00-0000-7000-8000-00000000000a",
  bob: "019a8f00-0000-7000-8000-00000000000b",
  left: "019a8f00-0005-7000-8000-000000000001",
  right: "019a8f00-0005-7000-8000-000000000002",
  collision: "019a8f00-0009-7000-8000-000000000001",
  objective: "019a8f00-0002-7000-8000-000000000001",
};

const names = buildNames({
  members: {
    project_id: ids.project,
    members: [ids.alice, ids.bob].map((human_id, i) => ({
      human: { human_id, github_user_id: i + 1, github_login: i === 0 ? "ana" : "luis" },
      membership: {
        project_id: ids.project,
        human_id,
        role: i === 0 ? "owner" : "member",
        added_at: "2026-11-20T09:00:00Z",
      },
    })),
  },
  intents: {
    project_id: ids.project,
    intents: [
      { id: ids.left, owner: ids.alice, summary: "# Retry failed payments\n\nDetails" },
      { id: ids.right, owner: ids.bob, summary: "- **Refund flow**" },
    ].map((intent) => ({
      status: "active" as const,
      owner_human_id: intent.owner,
      intent_revision: {
        intent_id: intent.id,
        project_id: ids.project,
        participant_id: intent.owner,
        revision: 1,
        objective_id: ids.objective,
        objective_revision: 1,
        summary_markdown: intent.summary,
        created_at: "2026-11-20T09:00:00Z",
      },
    })),
  },
  radar: {
    project: {
      project_id: ids.project,
      name: "Checkout reliability",
      owner_human_id: ids.alice,
      created_at: "2026-11-20T09:00:00Z",
    },
    as_of_event: 9,
    objectives: [{ objective_id: ids.objective, revision: 1, title: "Checkout retries" }],
    repositories: [],
    active_intents: [],
    open_collisions: [
      {
        collision_id: ids.collision,
        left_intent_id: ids.left,
        left_intent_revision: 1,
        right_intent_id: ids.right,
        right_intent_revision: 1,
        rule_id: "contract_producer_consumer",
        severity: "red",
        confidence: 0.9,
        state: "open",
        matched_subjects: ["contract:PaymentGateway", "contract:Refund"],
        explanation_markdown: "",
        created_at: "2026-11-20T09:00:00Z",
      },
    ],
    open_questions: [],
    assumptions: [],
    evidence_coverage: [],
    known_unknowns: [],
  },
});
const nobody = buildNames({});

const event = (
  event_type: string,
  entity_id: string,
  payload: Record<string, unknown> = {},
): EventEnvelope => ({
  project_id: ids.project,
  event_sequence: 7,
  event_id: "019a8f00-00ff-7000-8000-000000000001",
  event_type,
  entity_type: "x",
  entity_id,
  actor_human_id: ids.alice,
  occurred_at: "2026-11-20T09:00:00Z",
  payload,
});

describe("feed sentences", () => {
  it("covers the contract's event types", () => {
    expect(eventTypes.length).toBeGreaterThanOrEqual(22);
  });

  it.each(eventTypes)("%s has its own sentence without identifiers, even unresolved", (type) => {
    for (const directory of [names, nobody]) {
      const text = plainText(
        describeEvent(event(type, ids.collision, { revision: 2, outcome: "failing" }), directory)
          .segments,
      );
      expect(text).not.toMatch(/^Something happened/);
      expect(text).not.toMatch(UUID);
    }
    expect(familyOf(type)).toBeTruthy();
    expect(invalidationFor(type)).toBeDefined();
  });

  it.each(fixtureEvents)("shared fixture %s reads without identifiers", (_, fixture) => {
    const text = plainText(describeEvent(fixture, nobody).segments);
    expect(text).not.toMatch(UUID);
    expect(text.length).toBeGreaterThan(5);
  });

  it.each([
    [
      event("collision.opened", ids.collision),
      "@ana’s “Retry failed payments” now collides with @luis’s “Refund flow” on contract:PaymentGateway and 1 more",
      "warning",
    ],
    [
      event("project.member_added", ids.project, { human_id: ids.bob, role: "member" }),
      "@ana added @luis as a member",
      "neutral",
    ],
    [
      event("objective.revision_created", ids.objective, { revision: 2 }),
      "@ana published “Checkout retries” (revision 2)",
      "neutral",
    ],
    [
      event("combination.checked", ids.project, {
        collision_id: ids.collision,
        outcome: "merge_conflict",
      }),
      "Pair check of “Retry failed payments” with “Refund flow”: a merge conflict",
      "failing",
    ],
    [
      event("intent.released", ids.right),
      "@ana released “Refund flow” and laid it to rest",
      "neutral",
    ],
  ])("names records: %#", (input, text, tone) => {
    const line = describeEvent(input, names);
    expect(plainText(line.segments)).toBe(text);
    expect(line.tone).toBe(tone);
  });

  it("labels unresolved names instead of printing identifiers", () => {
    const text = plainText(describeEvent(event("intent.published", ids.left), nobody).segments);
    expect(text).toBe(`${FALLBACK.human} published “${FALLBACK.intent}”`);
  });

  it("reports unknown future event types without failing", () => {
    expect(plainText(describeEvent(event("thing.happened", ids.left), names).segments)).toBe(
      "Something happened: thing.happened",
    );
  });
});
