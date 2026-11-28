import type { ProjectQueryKind } from "../api/queries";

// Which read models a committed event can change. Views refetch those read
// models instead of re-deriving domain state in the browser, so the Lab can
// never disagree with the backend (web-lab-foundation requirement 7.6).

export interface Invalidation {
  project: ProjectQueryKind[];
  /** The signed-in Human's Project list. */
  projects: boolean;
}

/** Everything an Intent's lane, card, or Specimen page reads. */
const INTENT_VIEWS: ProjectQueryKind[] = ["board", "intent", "review"];

export function invalidationFor(eventType: string): Invalidation {
  if (eventType === "project.created" || eventType === "project.member_added") {
    return { project: ["members", "radar", "status"], projects: true };
  }
  if (eventType.startsWith("intent.") || eventType === "claims.observed") {
    return {
      project: ["intents", "radar", ...INTENT_VIEWS, "collision", "collisions"],
      projects: false,
    };
  }
  if (eventType.startsWith("collision.") || eventType.startsWith("resolution.")) {
    return {
      project: ["radar", ...INTENT_VIEWS, "collision", "collisions", "insights"],
      projects: false,
    };
  }
  if (eventType === "evidence.attached") {
    return { project: ["radar", ...INTENT_VIEWS, "evidence", "insights"], projects: false };
  }
  if (eventType === "combination.checked") {
    return { project: ["radar", "collision", "collisions", "review", "insights"], projects: false };
  }
  if (eventType.startsWith("objective.")) {
    return { project: ["radar", "board", "review", "status", "plans"], projects: false };
  }
  if (eventType.startsWith("blueprint.") || eventType.startsWith("charter.")) {
    return { project: ["radar", "status", "plans", "review"], projects: false };
  }
  if (eventType === "repository.connected") {
    return { project: ["radar"], projects: false };
  }
  // CI credentials change nothing the Lab shows.
  if (eventType.startsWith("ci_credential.")) {
    return { project: [], projects: false };
  }
  // A full refresh is cheap insurance for an unknown future event type.
  return {
    project: [
      "radar",
      "intents",
      "members",
      "board",
      "intent",
      "evidence",
      "review",
      "collision",
      "collisions",
      "status",
      "plans",
      "insights",
    ],
    projects: false,
  };
}
