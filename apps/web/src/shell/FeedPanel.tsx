import { type ReactNode, useState } from "react";
import type { UUID } from "../api/types";
import { BoltIcon, FlaskIcon, JarIcon, PersonIcon, StitchIcon } from "../components/icons";
import { EmptyState, LiveDot, RelativeTime, ThemedLabel } from "../components/ui";
import { describeEvent, FAMILY_LABELS, type Family, type Segment } from "../feed/sentences";
import { LABELS } from "../labels";
import { useProjectData } from "../project";
import { useProjectStream } from "../stream/ProjectStream";

const FAMILY_ICONS: Record<Family, ReactNode> = {
  intents: <FlaskIcon size={16} />,
  stitches: <StitchIcon size={16} />,
  evidence: <JarIcon size={16} />,
  planning: <BoltIcon size={16} />,
  project: <PersonIcon size={16} />,
};

const FAMILIES = Object.keys(FAMILY_LABELS) as Family[];

/** The Lightning feed: one sentence per committed event, newest first. */
export function FeedPanel({ projectId, compact = false }: { projectId: UUID; compact?: boolean }) {
  const { status, entries } = useProjectStream();
  const { names } = useProjectData(projectId);
  const [hidden, setHidden] = useState<ReadonlySet<Family>>(new Set());

  const toggle = (family: Family) =>
    setHidden((current) => {
      const next = new Set(current);
      if (next.has(family)) next.delete(family);
      else next.add(family);
      return next;
    });

  const lines = entries
    .map((entry) => ({ entry, line: describeEvent(entry.event, names) }))
    .filter(({ line }) => !hidden.has(line.family));

  return (
    <section className={compact ? "feed feed-compact" : "feed"}>
      <header className="feed-header">
        <ThemedLabel label={LABELS.feed} as="h2" className="feed-title" />
        <LiveDot state={status} />
      </header>
      {status === "delayed" && (
        <p className="notice notice-warning">
          The live connection is unavailable, so the feed is checked every few seconds instead.
        </p>
      )}
      <fieldset className="feed-filters">
        <legend className="sr-only">Show event families</legend>
        {FAMILIES.map((family) => (
          <button
            key={family}
            type="button"
            className="chip"
            aria-pressed={!hidden.has(family)}
            onClick={() => toggle(family)}
          >
            {FAMILY_ICONS[family]}
            {FAMILY_LABELS[family]}
          </button>
        ))}
      </fieldset>
      {lines.length === 0 ? (
        <EmptyState
          title={entries.length === 0 ? "No sparks yet." : "Nothing in the chosen families."}
        >
          {entries.length === 0
            ? "Events appear here the moment someone publishes, collides, decides, or proves something."
            : "Turn a family back on to see its events."}
        </EmptyState>
      ) : (
        <ol className="feed-list" aria-live="polite" aria-relevant="additions">
          {lines.map(({ entry, line }) => (
            <li
              key={entry.event.event_sequence}
              className={`feed-item tone-${line.tone}${entry.fresh ? " fresh" : ""}`}
            >
              <span className="feed-icon">{FAMILY_ICONS[line.family]}</span>
              <span className="feed-text">
                <Sentence segments={line.segments} />
                <span className="feed-time">
                  <RelativeTime value={entry.event.occurred_at} />
                </span>
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function Sentence({ segments }: { segments: Segment[] }) {
  return (
    <span>
      {segments.map((segment, index) => {
        // Segments are a fixed sequence per event, so the position is a stable key.
        const key = `${index}`;
        if (typeof segment === "string") return <span key={key}>{segment}</span>;
        if ("name" in segment) return <strong key={key}>{segment.name}</strong>;
        return (
          <code key={key} className="mono">
            {segment.code}
          </code>
        );
      })}
    </span>
  );
}
