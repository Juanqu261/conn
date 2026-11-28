import type { CollisionState, CombinationOutcome, EventEnvelope } from "../api/types";
import type { Names } from "../names/directory";

// One human sentence per committed Project event (web-lab-foundation
// requirement 7.4). Event payloads carry identifiers; every name comes from
// the name directory, and an unresolved name falls back to a labelled
// placeholder rather than an identifier.

export type Family = "intents" | "stitches" | "evidence" | "planning" | "project";
export type Tone = "neutral" | "warning" | "failing" | "alive";

/** A sentence is text with emphasized names and monospace references. */
export type Segment = string | { name: string } | { code: string };

export interface FeedLine {
  family: Family;
  tone: Tone;
  segments: Segment[];
}

export const FAMILY_LABELS: Record<Family, string> = {
  intents: "Intents",
  stitches: "Collisions and decisions",
  evidence: "Evidence",
  planning: "Planning",
  project: "Project",
};

export function familyOf(eventType: string): Family {
  if (eventType.startsWith("intent.") || eventType === "claims.observed") return "intents";
  if (
    eventType.startsWith("collision.") ||
    eventType.startsWith("resolution.") ||
    eventType === "combination.checked"
  ) {
    return "stitches";
  }
  if (eventType.startsWith("evidence.")) return "evidence";
  if (
    eventType.startsWith("objective.") ||
    eventType.startsWith("blueprint.") ||
    eventType.startsWith("charter.")
  ) {
    return "planning";
  }
  return "project";
}

const STATE_WORDS: Record<CollisionState, string> = {
  open: "open",
  under_review: "under review",
  resolved: "resolved",
  dismissed: "dismissed",
  stale: "stale",
};

const OUTCOME_WORDS: Record<CombinationOutcome, string> = {
  passing: "passing",
  failing: "failing",
  merge_conflict: "a merge conflict",
  incomplete: "incomplete",
};

const str = (value: unknown): string | undefined => (typeof value === "string" ? value : undefined);
const num = (value: unknown): number | undefined => (typeof value === "number" ? value : undefined);

/** Joins segments into plain text, for accessible names and tests. */
export function plainText(segments: Segment[]): string {
  return segments
    .map((segment) =>
      typeof segment === "string" ? segment : "name" in segment ? segment.name : segment.code,
    )
    .join("");
}

export function describeEvent(event: EventEnvelope, names: Names): FeedLine {
  const family = familyOf(event.event_type);
  const actor = { name: names.human(event.actor_human_id) };
  const payload = event.payload;
  const revision = num(payload.revision);
  const line = (segments: Segment[], tone: Tone = "neutral"): FeedLine => ({
    family,
    tone,
    segments,
  });
  const owned = (intentId: string | undefined): Segment[] => {
    const owner = names.intentOwner(intentId);
    const intent = { name: `“${names.intent(intentId)}”` };
    return owner ? [{ name: names.human(owner) }, "’s ", intent] : [intent];
  };

  switch (event.event_type) {
    case "project.created":
      return line([actor, " created the Project ", { name: names.project(event.project_id) }]);
    case "project.member_added": {
      const role = str(payload.role) === "owner" ? "an owner" : "a member";
      return line([actor, " added ", { name: names.human(str(payload.human_id)) }, ` as ${role}`]);
    }
    case "objective.imported":
      return line([
        actor,
        " imported ",
        { name: `“${names.objective(event.entity_id)}”` },
        revisionSuffix(revision),
      ]);
    case "objective.revision_created":
      return line([
        actor,
        " published ",
        { name: `“${names.objective(event.entity_id)}”` },
        revisionSuffix(revision),
      ]);
    case "blueprint.revision_proposed":
      return line([
        actor,
        ` proposed Blueprint revision ${revision ?? "?"} for `,
        { name: `“${names.objective(str(payload.objective_id))}”` },
      ]);
    case "blueprint.revision_activated":
      return line([`Blueprint revision ${revision ?? "?"} is now active`], "alive");
    case "blueprint.revision_rejected":
      return line(
        [
          actor,
          ` rejected a Cell Charter, so Blueprint revision ${revision ?? "?"} will not activate`,
        ],
        "warning",
      );
    case "charter.accepted":
      return line([
        actor,
        ` accepted their Cell Charter in Blueprint revision ${num(payload.blueprint_revision) ?? "?"}`,
      ]);
    case "charter.rejected":
      return line(
        [
          actor,
          ` rejected their Cell Charter in Blueprint revision ${num(payload.blueprint_revision) ?? "?"}`,
        ],
        "warning",
      );
    case "repository.connected":
      return line([actor, " connected ", { code: names.repository(event.entity_id) }]);
    case "intent.published":
      return line([actor, " published ", { name: `“${names.intent(event.entity_id)}”` }]);
    case "intent.revised":
      return line([
        actor,
        " revised ",
        { name: `“${names.intent(event.entity_id)}”` },
        revisionSuffix(revision),
      ]);
    case "intent.released":
      return line([
        actor,
        " released ",
        { name: `“${names.intent(event.entity_id)}”` },
        " and laid it to rest",
      ]);
    case "claims.observed":
      return line([
        actor,
        " analyzed the changes of ",
        ...owned(str(payload.intent_id)),
        revisionSuffix(num(payload.intent_revision)),
      ]);
    case "collision.opened": {
      const collision = names.collision(event.entity_id);
      if (!collision) {
        return line(["A new stitch joined two Intents"], "warning");
      }
      const [first, ...rest] = collision.matched_subjects;
      return line(
        [
          ...owned(collision.left_intent_id),
          " now collides with ",
          ...owned(collision.right_intent_id),
          " on ",
          { code: first ?? "a shared subject" },
          rest.length > 0 ? ` and ${rest.length} more` : "",
        ],
        "warning",
      );
    }
    case "collision.updated": {
      const collision = names.collision(event.entity_id);
      if (!collision) {
        return line(["A stitch changed state"]);
      }
      return line(
        [
          "The stitch between ",
          { name: `“${names.intent(collision.left_intent_id)}”` },
          " and ",
          { name: `“${names.intent(collision.right_intent_id)}”` },
          ` is ${STATE_WORDS[collision.state]}`,
        ],
        collision.state === "open" ? "warning" : "neutral",
      );
    }
    case "resolution.proposed":
      return line([actor, " proposed a resolution for a stitch (non-binding)"]);
    case "resolution.decided":
      return line(
        [
          actor,
          " decided a stitch",
          str(payload.decision_surface) === "browser" ? " in the Lab" : "",
        ],
        "alive",
      );
    case "evidence.attached":
      return line([actor, " attached evidence"]);
    case "combination.checked": {
      const outcome = str(payload.outcome) as CombinationOutcome | undefined;
      const collision = names.collision(str(payload.collision_id));
      const result = outcome ? OUTCOME_WORDS[outcome] : "finished";
      const tone: Tone =
        outcome === "passing"
          ? "alive"
          : outcome === "incomplete"
            ? "warning"
            : outcome
              ? "failing"
              : "neutral";
      if (!collision) {
        return line([`A pair check finished: ${result}`], tone);
      }
      return line(
        [
          "Pair check of ",
          { name: `“${names.intent(collision.left_intent_id)}”` },
          " with ",
          { name: `“${names.intent(collision.right_intent_id)}”` },
          `: ${result}`,
        ],
        tone,
      );
    }
    case "ci_credential.created":
      return line([
        actor,
        " created a CI credential for ",
        { code: names.repository(str(payload.repository_id)) },
      ]);
    case "ci_credential.revoked":
      return line([
        actor,
        " revoked a CI credential for ",
        { code: names.repository(str(payload.repository_id)) },
      ]);
    default:
      return line(["Something happened: ", { code: event.event_type }]);
  }
}

function revisionSuffix(revision: number | undefined): string {
  return revision && revision > 1 ? ` (revision ${revision})` : "";
}
