import type { ProjectQueryKind } from "../api/queries";

// Which read models a committed event can change. Views refetch those read
// models instead of re-deriving domain state in the browser, so the Lab can
// never disagree with the backend (web-lab-foundation requirement 7.6).

export interface Invalidation {
  project: ProjectQueryKind[];
  /** The signed-in Human's Project list. */
  projects: boolean;
}

export function invalidationFor(eventType: string): Invalidation {
  if (eventType === "project.created" || eventType === "project.member_added") {
    return { project: ["members", "radar"], projects: true };
  }
  if (eventType.startsWith("intent.") || eventType === "claims.observed") {
    return { project: ["intents", "radar"], projects: false };
  }
  if (
    eventType.startsWith("collision.") ||
    eventType.startsWith("resolution.") ||
    eventType.startsWith("objective.") ||
    eventType.startsWith("blueprint.") ||
    eventType.startsWith("charter.") ||
    eventType === "combination.checked" ||
    eventType === "evidence.attached" ||
    eventType === "repository.connected"
  ) {
    return { project: ["radar"], projects: false };
  }
  // CI credentials and unknown future events change nothing the Lab shows;
  // a full refresh is cheap insurance for an unknown type.
  if (eventType.startsWith("ci_credential.")) {
    return { project: [], projects: false };
  }
  return { project: ["radar", "intents", "members"], projects: false };
}
