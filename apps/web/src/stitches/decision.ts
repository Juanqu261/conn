import schema from "../../../../contracts/v1/resolution-decision-input.schema.json";
import type {
  CollisionAssessment,
  CollisionState,
  ResolutionDecisionInput,
  ResolutionKind,
} from "../api/types";

// The rules of a collision decision as Conn Lab offers it (web-lab-board
// requirement 5). Nothing here decides: the dialog offers only the kinds
// valid for the chosen column, never preselects one, and the backend
// enforces authority and freshness.

/** The columns a decision can move a stitch into. */
export type DecisionTarget = "under_review" | "resolved" | "dismissed";

export const TARGETS: DecisionTarget[] = ["under_review", "resolved", "dismissed"];

export const TARGET_LABELS: Record<DecisionTarget, string> = {
  under_review: "Under review",
  resolved: "Resolved",
  dismissed: "Dismissed",
};

const KINDS: Record<DecisionTarget, ResolutionKind[]> = {
  resolved: ["dependency", "sequencing", "contract_agreement", "accepted_risk"],
  dismissed: ["false_positive"],
  under_review: ["revise_objective", "revise_blueprint", "other"],
};

/** The decision kinds valid for a target column, in a fixed order. */
export function kindsFor(target: DecisionTarget): ResolutionKind[] {
  return [...KINDS[target]];
}

export const KIND_LABELS: Record<ResolutionKind, string> = {
  dependency: "Dependency",
  sequencing: "Sequencing",
  contract_agreement: "Contract agreement",
  accepted_risk: "Accepted risk",
  false_positive: "False positive",
  revise_objective: "Revise the Objective",
  revise_blueprint: "Revise the Blueprint",
  other: "Other",
};

/** What each kind means and what it does to the stitch. */
export const CONSEQUENCES: Record<ResolutionKind, string> = {
  dependency:
    "One Intent waits for the other. The stitch closes as Resolved; a later revision that adds a matched subject this decision does not cover reopens it.",
  sequencing:
    "The changes land in an agreed order. The stitch closes as Resolved; a later revision that adds a matched subject this decision does not cover reopens it.",
  contract_agreement:
    "Both Intents follow an agreed contract. Binding and owner-only. The stitch closes as Resolved; a new matched subject reopens it.",
  accepted_risk:
    "The team accepts the risk of both changes. Binding and owner-only. The stitch closes as Resolved; a new matched subject reopens it.",
  false_positive:
    "The rule matched, but the work does not interact. The stitch is Dismissed; a later revision keeps it dismissed only while its matched subjects stay covered.",
  revise_objective:
    "Escalates to a revision of the Objective. Binding and owner-only. The stitch moves to Under review until a new revision settles it.",
  revise_blueprint:
    "Escalates to a revision of the Blueprint. Binding and owner-only. The stitch moves to Under review until a new revision settles it.",
  other: "Another handling, explained in the rationale. The stitch moves to Under review.",
};

interface BindingRule {
  if: { properties: { kind: { enum: ResolutionKind[] } } };
}

/**
 * Kinds that are always binding, read from the resolution-decision-input
 * contract rather than redefined here, so the Lab binds exactly as conn app
 * does.
 */
export const ALWAYS_BINDING: ReadonlySet<ResolutionKind> = new Set(
  (schema.allOf as unknown as BindingRule[]).flatMap((rule) => rule.if.properties.kind.enum),
);

/** Only open and under-review stitches take a decision from the board. */
export function decidable(state: CollisionState): boolean {
  return state === "open" || state === "under_review";
}

/** The columns a card may be dropped onto; empty for cards that cannot move. */
export function dropTargets(state: CollisionState): DecisionTarget[] {
  return decidable(state) ? [...TARGETS] : [];
}

export type BindingChoice = "record" | "binding";

export interface DecisionDraft {
  target: DecisionTarget | undefined;
  kind: ResolutionKind | undefined;
  binding: BindingChoice;
  proposalId: string | undefined;
  rationale: string;
}

/**
 * The dialog's starting state. A drop names the target column; the keyboard
 * action leaves it for the member to choose. The kind is never preselected,
 * even when a proposal suggests one (requirements 5.3 and 7.4).
 */
export function initialDraft(target?: DecisionTarget): DecisionDraft {
  return { target, kind: undefined, binding: "record", proposalId: undefined, rationale: "" };
}

/** Whether the decision will be binding, as conn app sets it. */
export function isBinding(kind: ResolutionKind, choice: BindingChoice): boolean {
  return ALWAYS_BINDING.has(kind) || choice === "binding";
}

/** Why the draft cannot be recorded yet, or undefined when it can. */
export function draftProblem(
  draft: DecisionDraft,
  owner: boolean,
  proposalKind: ResolutionKind | undefined,
): string | undefined {
  if (!draft.target) return "Choose the column this stitch moves to.";
  if (!draft.kind) return "Choose a decision kind.";
  if (!kindsFor(draft.target).includes(draft.kind)) return "That kind does not fit this column.";
  if (!owner && isBinding(draft.kind, draft.binding)) {
    return "Only the Project owner can record a binding decision.";
  }
  if (proposalKind && proposalKind !== draft.kind) {
    return `The selected proposal suggests ${KIND_LABELS[proposalKind].toLowerCase()}; a decision based on it must use that kind, or stop basing it on the proposal.`;
  }
  if (!draft.rationale.trim()) return "Write the rationale for this decision.";
  return undefined;
}

/** The request body, with the exact revisions and state the member reviewed. */
export function decisionInput(
  draft: DecisionDraft & { kind: ResolutionKind },
  collision: CollisionAssessment,
): ResolutionDecisionInput {
  const input: ResolutionDecisionInput = {
    kind: draft.kind,
    binding: isBinding(draft.kind, draft.binding),
    rationale_markdown: draft.rationale.trim(),
    expected_left_intent_revision: collision.left_intent_revision,
    expected_right_intent_revision: collision.right_intent_revision,
    expected_state: collision.state,
  };
  if (draft.proposalId) input.selected_proposal_id = draft.proposalId;
  return input;
}
