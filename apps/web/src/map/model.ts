import type { CollisionAssessment, RadarView, UUID } from "../api/types";
import { type Names, summaryLine } from "../names/directory";

// The Stitch Map's graph, built from one Radar snapshot (requirement 8):
// active Intents clustered by Objective, collisions as edges, and an
// optional layer of matched subjects. Pure, so the table view and the
// graph show exactly the same items.

export const MAX_INTENTS = 150;
export const MAX_SUBJECTS = 150;

export interface MapNode {
  id: string;
  kind: "objective" | "intent" | "subject";
  label: string;
  parent?: string;
  intentId?: UUID;
  ownerHumanId?: UUID;
}

export interface MapEdge {
  id: string;
  kind: "collision" | "claim";
  source: string;
  target: string;
  collision?: CollisionAssessment;
}

export interface MapModel {
  nodes: MapNode[];
  edges: MapEdge[];
  /** Plain sentences about what the graph leaves out. */
  omissions: string[];
}

export interface MapOptions {
  subjects: boolean;
  focus?: UUID | undefined;
}

const intentNode = (id: UUID) => `intent:${id}`;

export function buildMap(radar: RadarView, names: Names, options: MapOptions): MapModel {
  const omissions: string[] = [];
  const collisions = [...radar.open_collisions, ...(radar.decided_collisions ?? [])].filter(
    (collision) => collision.state !== "stale",
  );
  let intents = radar.active_intents;
  if (options.focus) {
    const neighbours = new Set<UUID>([options.focus]);
    for (const collision of collisions) {
      if (collision.left_intent_id === options.focus) neighbours.add(collision.right_intent_id);
      if (collision.right_intent_id === options.focus) neighbours.add(collision.left_intent_id);
    }
    intents = intents.filter((intent) => neighbours.has(intent.intent_id));
  }
  // Intents with stitches first, so a bound keeps the interesting part.
  const stitched = new Set(collisions.flatMap((c) => [c.left_intent_id, c.right_intent_id]));
  intents = [...intents].sort(
    (a, b) => Number(stitched.has(b.intent_id)) - Number(stitched.has(a.intent_id)),
  );
  if (intents.length > MAX_INTENTS) {
    omissions.push(
      `${intents.length - MAX_INTENTS} of ${intents.length} active Intents are not drawn; the table lists every stitch.`,
    );
    intents = intents.slice(0, MAX_INTENTS);
  }
  const shown = new Set(intents.map((intent) => intent.intent_id));

  const nodes: MapNode[] = [];
  const objectives = new Set(intents.map((intent) => intent.objective_id));
  for (const objective of objectives) {
    nodes.push({
      id: `objective:${objective}`,
      kind: "objective",
      label: names.objective(objective),
    });
  }
  for (const intent of intents) {
    const node: MapNode = {
      id: intentNode(intent.intent_id),
      kind: "intent",
      label: summaryLine(intent.summary_markdown),
      parent: `objective:${intent.objective_id}`,
      intentId: intent.intent_id,
    };
    const owner = names.intentOwner(intent.intent_id);
    if (owner) node.ownerHumanId = owner;
    nodes.push(node);
  }

  const edges: MapEdge[] = [];
  let hiddenEdges = 0;
  for (const collision of collisions) {
    if (!shown.has(collision.left_intent_id) || !shown.has(collision.right_intent_id)) {
      hiddenEdges++;
      continue;
    }
    edges.push({
      id: `collision:${collision.collision_id}`,
      kind: "collision",
      source: intentNode(collision.left_intent_id),
      target: intentNode(collision.right_intent_id),
      collision,
    });
  }
  if (hiddenEdges > 0 && !options.focus) {
    omissions.push(`${hiddenEdges} stitches join Intents that are not drawn.`);
  }
  if ((radar.decided_collisions?.length ?? 0) >= 50) {
    omissions.push("Only the 50 most recent decided stitches are included.");
  }

  if (options.subjects) {
    const subjects = new Map<string, Set<UUID>>();
    for (const edge of edges) {
      const collision = edge.collision;
      if (!collision) continue;
      for (const subject of collision.matched_subjects) {
        const claimants = subjects.get(subject) ?? new Set<UUID>();
        claimants.add(collision.left_intent_id);
        claimants.add(collision.right_intent_id);
        subjects.set(subject, claimants);
      }
    }
    let count = 0;
    for (const [subject, claimants] of [...subjects].sort(([a], [b]) => a.localeCompare(b))) {
      if (count === MAX_SUBJECTS) {
        omissions.push(`${subjects.size - MAX_SUBJECTS} matched subjects are not drawn.`);
        break;
      }
      count++;
      nodes.push({ id: `subject:${subject}`, kind: "subject", label: subject });
      for (const intentId of claimants) {
        edges.push({
          id: `claim:${subject}:${intentId}`,
          kind: "claim",
          source: `subject:${subject}`,
          target: intentNode(intentId),
        });
      }
    }
  }
  return { nodes, edges, omissions };
}

/** Intent nodes in a stable order for keyboard traversal: by Objective, then name. */
export function traversalOrder(model: MapModel, names: Names): MapNode[] {
  return model.nodes
    .filter((node) => node.kind === "intent")
    .sort((a, b) => {
      const objective = names
        .objective(a.parent?.slice(10))
        .localeCompare(names.objective(b.parent?.slice(10)));
      return objective || a.label.localeCompare(b.label);
    });
}
