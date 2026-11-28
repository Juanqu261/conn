import { useQueryClient } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useEffect, useState } from "react";
import { apiGet } from "../api/client";
import { eventsPath, type ProjectQueryKind, projectKey, streamPath } from "../api/queries";
import type { EventEnvelope, UUID } from "../api/types";
import { invalidationFor } from "./invalidation";

// One live connection per open Project (web-lab-foundation requirement 7).
// The Lab opens the Server-Sent Events stream after the last cursor it
// rendered; the browser resumes it with Last-Event-ID. When the stream is
// refused or unavailable, the Lab pulls GET /events by cursor while the tab
// is visible and says the feed is delayed, then retries the stream.

export type StreamStatus = "connecting" | "live" | "delayed";

export interface FeedEntry {
  event: EventEnvelope;
  /** True for events that arrived while the page was open. */
  fresh: boolean;
}

interface StreamState {
  status: StreamStatus;
  entries: FeedEntry[];
}

const FEED_LIMIT = 200;
const BACKLOG = 50;
const POLL_MS = 10_000;
const STREAM_RETRY_MS = 60_000;
const INVALIDATE_DEBOUNCE_MS = 250;

const StreamContext = createContext<StreamState>({ status: "connecting", entries: [] });

export function useProjectStream(): StreamState {
  return useContext(StreamContext);
}

export function ProjectStreamProvider({
  projectId,
  children,
}: {
  projectId: UUID;
  children: ReactNode;
}) {
  const queryClient = useQueryClient();
  const [state, setState] = useState<StreamState>({ status: "connecting", entries: [] });

  useEffect(() => {
    let cancelled = false;
    let cursor = 0;
    let source: EventSource | undefined;
    let pollTimer: number | undefined;
    let retryTimer: number | undefined;
    let flushTimer: number | undefined;
    const pendingKinds = new Set<ProjectQueryKind>();
    let pendingProjects = false;
    setState({ status: "connecting", entries: [] });

    const setStatus = (status: StreamStatus) => {
      if (!cancelled)
        setState((current) => (current.status === status ? current : { ...current, status }));
    };

    const flush = () => {
      for (const kind of pendingKinds) {
        void queryClient.invalidateQueries({ queryKey: projectKey(kind, projectId) });
      }
      if (pendingProjects) {
        void queryClient.invalidateQueries({ queryKey: ["projects"] });
      }
      pendingKinds.clear();
      pendingProjects = false;
    };

    const accept = (events: EventEnvelope[], fresh: boolean) => {
      const newer = events
        .filter((event) => event.event_sequence > cursor)
        .sort((a, b) => a.event_sequence - b.event_sequence);
      const last = newer.at(-1);
      if (cancelled || !last) return;
      cursor = last.event_sequence;
      if (fresh) {
        for (const event of newer) {
          const invalidation = invalidationFor(event.event_type);
          for (const kind of invalidation.project) pendingKinds.add(kind);
          pendingProjects ||= invalidation.projects;
        }
        window.clearTimeout(flushTimer);
        flushTimer = window.setTimeout(flush, INVALIDATE_DEBOUNCE_MS);
      }
      const added = newer.reverse().map((event) => ({ event, fresh }));
      setState((current) => ({
        ...current,
        entries: [...added, ...current.entries].slice(0, FEED_LIMIT),
      }));
    };

    const loadBacklog = async () => {
      const head = await apiGet<EventEnvelope[]>(eventsPath(projectId, 0, 1));
      const last = head.as_of_event ?? 0;
      const start = Math.max(0, last - BACKLOG);
      const page = await apiGet<EventEnvelope[]>(eventsPath(projectId, start, BACKLOG));
      accept(page.data, false);
      cursor = Math.max(cursor, last);
    };

    const poll = async () => {
      if (cancelled || document.visibilityState === "hidden") return;
      try {
        accept((await apiGet<EventEnvelope[]>(eventsPath(projectId, cursor, 100))).data, true);
      } catch {
        // The next poll or stream retry tries again.
      }
    };

    const stopPolling = () => {
      window.clearInterval(pollTimer);
      pollTimer = undefined;
    };

    const fallBack = () => {
      setStatus("delayed");
      if (pollTimer === undefined) {
        pollTimer = window.setInterval(() => void poll(), POLL_MS);
        void poll();
      }
      window.clearTimeout(retryTimer);
      retryTimer = window.setTimeout(connect, STREAM_RETRY_MS);
    };

    const connect = () => {
      if (cancelled) return;
      source?.close();
      if (typeof EventSource === "undefined") {
        fallBack();
        return;
      }
      const stream = new EventSource(streamPath(projectId, cursor));
      source = stream;
      stream.onopen = () => {
        stopPolling();
        window.clearTimeout(retryTimer);
        setStatus("live");
      };
      stream.onmessage = (message) => {
        try {
          accept([JSON.parse(message.data) as EventEnvelope], true);
        } catch {
          // A malformed message is skipped; the cursor still advances on the next.
        }
      };
      // More history than the stream replays: reload every read model, then
      // continue from the server's cursor.
      stream.addEventListener("resync", () => {
        stream.close();
        void queryClient.invalidateQueries({
          predicate: (query) => query.queryKey[1] === projectId,
        });
        setState((current) => ({ ...current, entries: [] }));
        cursor = 0;
        void loadBacklog()
          .catch(() => undefined)
          .finally(connect);
      });
      stream.onerror = () => {
        if (stream.readyState === EventSource.CLOSED) {
          fallBack();
        } else {
          setStatus("connecting");
        }
      };
    };

    void loadBacklog()
      .catch(() => undefined)
      .finally(connect);

    return () => {
      cancelled = true;
      source?.close();
      stopPolling();
      window.clearTimeout(retryTimer);
      window.clearTimeout(flushTimer);
    };
  }, [projectId, queryClient]);

  return <StreamContext.Provider value={state}>{children}</StreamContext.Provider>;
}
