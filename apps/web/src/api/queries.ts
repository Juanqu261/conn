import { queryOptions } from "@tanstack/react-query";
import { apiGet } from "./client";
import type {
  IntentList,
  ProjectList,
  ProjectMemberList,
  RadarView,
  UUID,
  WebLabStatus,
  WebSession,
} from "./types";

/** Read models scoped to one Project; the stream invalidates them by kind. */
export type ProjectQueryKind = "radar" | "intents" | "members";

export const projectKey = (kind: ProjectQueryKind, projectId: UUID) => [kind, projectId] as const;

const path = (projectId: UUID, rest: string) =>
  `/v1/projects/${encodeURIComponent(projectId)}${rest}`;

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

export const eventsPath = (projectId: UUID, after: number, limit: number) =>
  path(projectId, `/events?after=${after}&limit=${limit}`);

export const streamPath = (projectId: UUID, after: number) =>
  path(projectId, `/events/stream?after=${after}`);
