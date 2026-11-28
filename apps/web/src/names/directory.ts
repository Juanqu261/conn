import type {
  CollisionAssessment,
  CollisionRule,
  Human,
  IntentList,
  ProjectList,
  ProjectMemberList,
  RadarView,
  UUID,
} from "../api/types";

// Names, not identifiers: Conn Lab names records the way people know them
// and never prints a raw UUID inline (web-lab-foundation requirement 5).
// This is the web counterpart of internal/tui/names.go.

export const FALLBACK = {
  human: "Unknown person",
  intent: "Unknown Intent",
  objective: "Unknown Objective",
  repository: "Unknown repository",
  project: "Unknown Project",
  collision: "Unknown stitch",
} as const;

export const RULE_LABELS: Record<CollisionRule, string> = {
  shared_declared_invariant: "Shared invariant",
  shared_exported_symbol: "Shared export",
  contract_producer_consumer: "Contract producer and consumer",
  schema_resource_overlap: "Shared schema or resource",
};

export interface IntentName {
  name: string;
  ownerHumanId?: UUID;
}

export interface Names {
  human(id: UUID | undefined): string;
  humanRecord(id: UUID | undefined): Human | undefined;
  intent(id: UUID | undefined): string;
  intentOwner(id: UUID | undefined): string | undefined;
  objective(id: UUID | undefined): string;
  repository(id: UUID | undefined): string;
  project(id: UUID | undefined): string;
  collision(id: UUID | undefined): CollisionAssessment | undefined;
  collisionName(collision: CollisionAssessment): string;
}

export interface DirectorySources {
  projects?: ProjectList | undefined;
  members?: ProjectMemberList | undefined;
  intents?: IntentList | undefined;
  radar?: RadarView | undefined;
}

/**
 * The first meaningful line of an Intent summary, without Markdown heading,
 * list, or emphasis markers, bounded for display.
 */
export function summaryLine(markdown: string): string {
  for (const raw of markdown.split(/\r?\n/)) {
    const line = raw
      .replace(/^\s*(#{1,6}\s+|[-*+]\s+|\d+[.)]\s+|>\s*)/, "")
      .replace(/[*_`~]/g, "")
      .trim();
    if (line) {
      return line.length > 120 ? `${line.slice(0, 119)}…` : line;
    }
  }
  return FALLBACK.intent;
}

export function buildNames(sources: DirectorySources): Names {
  const humans = new Map<UUID, Human>();
  for (const member of sources.members?.members ?? []) {
    humans.set(member.human.human_id, member.human);
  }
  const intents = new Map<UUID, IntentName>();
  for (const view of sources.intents?.intents ?? []) {
    const entry: IntentName = { name: summaryLine(view.intent_revision.summary_markdown) };
    if (view.owner_human_id) {
      entry.ownerHumanId = view.owner_human_id;
    }
    intents.set(view.intent_revision.intent_id, entry);
  }
  // Radar's active revisions are at least as new as the Intent list.
  for (const revision of sources.radar?.active_intents ?? []) {
    const known = intents.get(revision.intent_id);
    intents.set(revision.intent_id, { ...known, name: summaryLine(revision.summary_markdown) });
  }
  const objectives = new Map<UUID, string>();
  for (const objective of sources.radar?.objectives ?? []) {
    objectives.set(objective.objective_id, objective.title);
  }
  const repositories = new Map<UUID, string>();
  for (const repository of sources.radar?.repositories ?? []) {
    repositories.set(repository.repository_id, repository.canonical_remote);
  }
  const projects = new Map<UUID, string>();
  for (const entry of sources.projects?.projects ?? []) {
    projects.set(entry.project.project_id, entry.project.name);
  }
  if (sources.radar) {
    projects.set(sources.radar.project.project_id, sources.radar.project.name);
  }
  const collisions = new Map<UUID, CollisionAssessment>();
  for (const collision of sources.radar?.open_collisions ?? []) {
    collisions.set(collision.collision_id, collision);
  }

  const intent = (id: UUID | undefined) => (id && intents.get(id)?.name) || FALLBACK.intent;
  return {
    human: (id) => {
      const login = id ? humans.get(id)?.github_login : undefined;
      return login ? `@${login}` : FALLBACK.human;
    },
    humanRecord: (id) => (id ? humans.get(id) : undefined),
    intent,
    intentOwner: (id) => (id ? intents.get(id)?.ownerHumanId : undefined),
    objective: (id) => (id && objectives.get(id)) || FALLBACK.objective,
    repository: (id) => (id && repositories.get(id)) || FALLBACK.repository,
    project: (id) => (id && projects.get(id)) || FALLBACK.project,
    collision: (id) => (id ? collisions.get(id) : undefined),
    collisionName: (collision) =>
      `${RULE_LABELS[collision.rule_id] ?? "Collision"}: ${intent(collision.left_intent_id)} ⇄ ${intent(collision.right_intent_id)}`,
  };
}

/** A stable Project URL segment: a cosmetic name prefix, then the ID. */
export function projectSlug(projectId: UUID, name: string): string {
  const prefix = name
    .toLowerCase()
    .normalize("NFKD")
    .replace(/\p{M}/gu, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
  return prefix ? `${prefix}~${projectId}` : projectId;
}

/** The Project ID in a URL segment; the router ignores the name prefix. */
export function projectIdFromSlug(slug: string): UUID {
  const at = slug.lastIndexOf("~");
  return at >= 0 ? slug.slice(at + 1) : slug;
}
