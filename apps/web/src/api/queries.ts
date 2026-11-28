import { queryOptions } from "@tanstack/react-query";
import { apiGet, apiGetText } from "./client";
import type {
  BlueprintRevisionView,
  BoardView,
  CollisionList,
  CollisionState,
  ConflictContextPack,
  EvidenceReferenceList,
  IntentList,
  IntentView,
  Objective,
  ProjectInsights,
  ProjectList,
  ProjectMemberList,
  ProjectStatus,
  RadarView,
  ResolutionDecision,
  ResolutionProposalList,
  ReviewPacket,
  UUID,
  WebLabStatus,
  WebSession,
} from "./types";

/**
 * Read models scoped to one Project; the stream invalidates them by kind.
 * Every key starts with [kind, projectId], so invalidating a kind also
 * refreshes its per-record queries.
 */
export type ProjectQueryKind =
  | "radar"
  | "intents"
  | "members"
  | "board"
  | "intent"
  | "evidence"
  | "review"
  | "collision"
  | "collisions"
  | "status"
  | "plans"
  | "insights";

export const projectKey = (kind: ProjectQueryKind, projectId: UUID) => [kind, projectId] as const;

const path = (projectId: UUID, rest: string) =>
  `/v1/projects/${encodeURIComponent(projectId)}${rest}`;

const id = encodeURIComponent;

export const sessionQuery = queryOptions({
  queryKey: ["session"],
  queryFn: async () => (await apiGet<WebSession>("/v1/web/session")).data,
  retry: false,
  staleTime: 5 * 60_000,
});

export const labStatusQuery = queryOptions({
  queryKey: ["lab"],
  queryFn: async () => (await apiGet<WebLabStatus>("/v1/web/lab")).data,
  staleTime: 60_000,
});

export const projectsQuery = queryOptions({
  queryKey: ["projects"],
  queryFn: async () => (await apiGet<ProjectList>("/v1/projects")).data,
});

export const radarQuery = (projectId: UUID) =>
  queryOptions({
    queryKey: projectKey("radar", projectId),
    queryFn: async () => (await apiGet<RadarView>(path(projectId, "/radar"))).data,
  });

export const intentsQuery = (projectId: UUID) =>
  queryOptions({
    queryKey: projectKey("intents", projectId),
    queryFn: async () => (await apiGet<IntentList>(path(projectId, "/intents"))).data,
  });

export const membersQuery = (projectId: UUID) =>
  queryOptions({
    queryKey: projectKey("members", projectId),
    queryFn: async () => (await apiGet<ProjectMemberList>(path(projectId, "/members"))).data,
  });

export const boardQuery = (projectId: UUID, windowDays = 14) =>
  queryOptions({
    queryKey: [...projectKey("board", projectId), windowDays] as const,
    queryFn: async () =>
      (await apiGet<BoardView>(path(projectId, `/board?window_days=${windowDays}`))).data,
  });

/** One Intent, at its current revision or an exact earlier one. */
export const intentQuery = (projectId: UUID, intentId: UUID, revision?: number) =>
  queryOptions({
    queryKey: [...projectKey("intent", projectId), intentId, revision ?? "current"] as const,
    queryFn: async () =>
      (
        await apiGet<IntentView>(
          path(projectId, `/intents/${id(intentId)}${revision ? `?revision=${revision}` : ""}`),
        )
      ).data,
  });

export const evidenceQuery = (projectId: UUID, intentId: UUID) =>
  queryOptions({
    queryKey: [...projectKey("evidence", projectId), intentId] as const,
    queryFn: async () =>
      (await apiGet<EvidenceReferenceList>(path(projectId, `/intents/${id(intentId)}/evidence`)))
        .data,
  });

export const reviewPacketQuery = (projectId: UUID, intentId: UUID) =>
  queryOptions({
    queryKey: [...projectKey("review", projectId), intentId, "json"] as const,
    queryFn: async () =>
      (
        await apiGet<ReviewPacket>(
          path(projectId, `/intents/${id(intentId)}/review-packet?format=json`),
        )
      ).data,
  });

export const reviewMarkdownQuery = (projectId: UUID, intentId: UUID) =>
  queryOptions({
    queryKey: [...projectKey("review", projectId), intentId, "markdown"] as const,
    queryFn: () =>
      apiGetText(path(projectId, `/intents/${id(intentId)}/review-packet?format=markdown`)),
  });

/** A collision in any state, with both Intents, claims, and pair checks. */
export const collisionContextQuery = (projectId: UUID, collisionId: UUID) =>
  queryOptions({
    queryKey: [...projectKey("collision", projectId), collisionId, "context"] as const,
    queryFn: async () =>
      (await apiGet<ConflictContextPack>(path(projectId, `/collisions/${id(collisionId)}/context`)))
        .data,
  });

export const proposalsQuery = (projectId: UUID, collisionId: UUID) =>
  queryOptions({
    queryKey: [...projectKey("collision", projectId), collisionId, "proposals"] as const,
    queryFn: async () =>
      (
        await apiGet<ResolutionProposalList>(
          path(projectId, `/collisions/${id(collisionId)}/proposals`),
        )
      ).data,
  });

export const decisionQuery = (projectId: UUID, decisionId: UUID) =>
  queryOptions({
    queryKey: [...projectKey("collision", projectId), "decision", decisionId] as const,
    queryFn: async () =>
      (await apiGet<ResolutionDecision>(path(projectId, `/decisions/${id(decisionId)}`))).data,
    // Decisions are immutable.
    staleTime: Number.POSITIVE_INFINITY,
  });

export const collisionsInStateQuery = (projectId: UUID, state: CollisionState) =>
  queryOptions({
    queryKey: [...projectKey("collisions", projectId), state] as const,
    queryFn: async () =>
      (await apiGet<CollisionList>(path(projectId, `/collisions?state=${state}&limit=50`))).data,
  });

export const statusQuery = (projectId: UUID) =>
  queryOptions({
    queryKey: projectKey("status", projectId),
    queryFn: async () => (await apiGet<ProjectStatus>(path(projectId, "/status"))).data,
  });

export const objectiveRevisionQuery = (projectId: UUID, objectiveId: UUID, revision: number) =>
  queryOptions({
    queryKey: [...projectKey("plans", projectId), "objective", objectiveId, revision] as const,
    queryFn: async () =>
      (
        await apiGet<Objective>(
          path(projectId, `/objectives/${id(objectiveId)}/revisions/${revision}`),
        )
      ).data,
  });

export const blueprintRevisionQuery = (projectId: UUID, blueprintId: UUID, revision: number) =>
  queryOptions({
    queryKey: [...projectKey("plans", projectId), "blueprint", blueprintId, revision] as const,
    queryFn: async () =>
      (
        await apiGet<BlueprintRevisionView>(
          path(projectId, `/blueprints/${id(blueprintId)}/revisions/${revision}`),
        )
      ).data,
  });

export const insightsQuery = (projectId: UUID, sinceDays: number) =>
  queryOptions({
    queryKey: [...projectKey("insights", projectId), sinceDays] as const,
    queryFn: async () =>
      (await apiGet<ProjectInsights>(path(projectId, `/insights?since_days=${sinceDays}`))).data,
  });

export const decisionsPath = (projectId: UUID, collisionId: UUID) =>
  path(projectId, `/collisions/${id(collisionId)}/decisions`);

export const charterDecisionPath = (
  projectId: UUID,
  blueprintId: UUID,
  revision: number,
  cellId: UUID,
  outcome: "accept" | "reject",
) =>
  path(
    projectId,
    `/blueprints/${id(blueprintId)}/revisions/${revision}/cells/${id(cellId)}/${outcome}`,
  );

export const eventsPath = (projectId: UUID, after: number, limit: number) =>
  path(projectId, `/events?after=${after}&limit=${limit}`);

export const streamPath = (projectId: UUID, after: number) =>
  path(projectId, `/events/stream?after=${after}`);
