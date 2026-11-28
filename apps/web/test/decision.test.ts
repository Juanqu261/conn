import { describe, expect, it } from "vitest";
import type { CollisionAssessment } from "../src/api/types";
import {
  ALWAYS_BINDING,
  decisionInput,
  draftProblem,
  dropTargets,
  initialDraft,
  kindsFor,
  TARGETS,
} from "../src/stitches/decision";

// Protects web-lab-board requirements 5 and 7.4: the decision dialog offers
// only the kinds valid for the target column, never preselects one (even
// when a proposal suggests it), binds as conn app does, and always sends the
// exact revisions and state the member reviewed.

const collision: CollisionAssessment = {
  collision_id: "019a8f00-0009-7000-8000-000000000001",
  left_intent_id: "019a8f00-0005-7000-8000-000000000001",
  left_intent_revision: 3,
  right_intent_id: "019a8f00-0005-7000-8000-000000000002",
  right_intent_revision: 5,
  rule_id: "contract_producer_consumer",
  severity: "red",
  confidence: 0.92,
  state: "open",
  matched_subjects: ["contract:PaymentGateway"],
  explanation_markdown: "Producer and consumer of one contract.",
  created_at: "2026-11-28T09:00:00Z",
};

describe("decision dialog", () => {
  it.each([
    ["resolved", ["dependency", "sequencing", "contract_agreement", "accepted_risk"]],
    ["dismissed", ["false_positive"]],
    ["under_review", ["revise_objective", "revise_blueprint", "other"]],
  ] as const)("offers only the %s kinds", (target, kinds) => {
    expect(kindsFor(target)).toEqual(kinds);
  });

  it("never preselects a kind, from a drop or from the keyboard", () => {
    for (const target of [...TARGETS, undefined]) {
      const draft = initialDraft(target);
      expect(draft.kind).toBeUndefined();
      expect(draft.proposalId).toBeUndefined();
      expect(draft.target).toBe(target);
    }
  });

  it("refuses a kind the column does not offer and a missing rationale", () => {
    expect(
      draftProblem(
        { ...initialDraft("dismissed"), kind: "sequencing", rationale: "x" },
        true,
        undefined,
      ),
    ).toMatch(/does not fit/);
    expect(
      draftProblem({ ...initialDraft("dismissed"), kind: "false_positive" }, true, undefined),
    ).toMatch(/rationale/);
  });

  it("binds exactly the kinds the contract marks as always binding", () => {
    expect([...ALWAYS_BINDING].sort()).toEqual(
      ["accepted_risk", "contract_agreement", "revise_blueprint", "revise_objective"].sort(),
    );
    const draft = {
      ...initialDraft("resolved"),
      kind: "accepted_risk" as const,
      rationale: "Known risk.",
    };
    expect(draftProblem(draft, false, undefined)).toMatch(/owner/);
    expect(decisionInput(draft, collision).binding).toBe(true);
  });

  it("sends the revisions and state the member reviewed", () => {
    const draft = {
      ...initialDraft("resolved"),
      kind: "sequencing" as const,
      rationale: " Payments first. ",
    };
    expect(decisionInput(draft, collision)).toEqual({
      kind: "sequencing",
      binding: false,
      rationale_markdown: "Payments first.",
      expected_left_intent_revision: 3,
      expected_right_intent_revision: 5,
      expected_state: "open",
    });
  });

  it("never lets a decided or stale stitch be dragged", () => {
    expect(dropTargets("resolved")).toEqual([]);
    expect(dropTargets("dismissed")).toEqual([]);
    expect(dropTargets("stale")).toEqual([]);
    expect(dropTargets("open")).toEqual(TARGETS);
  });
});
