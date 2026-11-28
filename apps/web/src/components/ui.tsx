import { DropdownMenu } from "radix-ui";
import { type ReactNode, useEffect, useState } from "react";
import type { CollisionState, CombinationOutcome, EvidenceState, Human } from "../api/types";
import type { Label } from "../labels";
import { usePlainLabels } from "../theme/preferences";
import {
  BoltIcon,
  CheckIcon,
  CircleIcon,
  CrossIcon,
  DiamondIcon,
  DotIcon,
  MoreIcon,
  TombstoneIcon,
} from "./icons";

/** A themed name with its plain caption; the plain wording is the accessible name. */
export function ThemedLabel({
  label,
  as: Tag = "span",
  className,
}: {
  label: Label;
  as?: "span" | "h1" | "h2" | "h3";
  className?: string;
}) {
  const plain = usePlainLabels();
  if (plain || label.themed === label.plain) {
    return <Tag className={className}>{label.plain}</Tag>;
  }
  return (
    <Tag className={["themed", className].filter(Boolean).join(" ")}>
      <span className="sr-only">{label.plain}</span>
      <span className="themed-name" aria-hidden="true">
        {label.themed}
      </span>
      <span className="themed-caption" aria-hidden="true">
        {label.plain}
      </span>
    </Tag>
  );
}

type Tone = "warning" | "review" | "failing" | "alive" | "neutral";

const COLLISION_STATES: Record<CollisionState, { tone: Tone; text: string; icon: ReactNode }> = {
  open: { tone: "warning", text: "Open", icon: <CircleIcon size={14} /> },
  under_review: { tone: "review", text: "Under review", icon: <DiamondIcon size={14} /> },
  resolved: { tone: "alive", text: "Resolved", icon: <CheckIcon size={14} /> },
  dismissed: { tone: "neutral", text: "Dismissed", icon: <CrossIcon size={14} /> },
  stale: { tone: "neutral", text: "Stale", icon: <TombstoneIcon size={14} /> },
};

const EVIDENCE_STATES: Record<EvidenceState, { tone: Tone; text: string; icon: ReactNode }> = {
  passing: { tone: "alive", text: "Passing", icon: <CheckIcon size={14} /> },
  failing: { tone: "failing", text: "Failing", icon: <CrossIcon size={14} /> },
  missing: { tone: "neutral", text: "Missing", icon: <CircleIcon size={14} /> },
  stale: { tone: "warning", text: "Stale", icon: <TombstoneIcon size={14} /> },
  inaccessible: { tone: "failing", text: "Inaccessible", icon: <CrossIcon size={14} /> },
  unknown: { tone: "neutral", text: "Unknown", icon: <CircleIcon size={14} /> },
};

const OUTCOMES: Record<CombinationOutcome, { tone: Tone; text: string; icon: ReactNode }> = {
  passing: { tone: "alive", text: "Pair check passing", icon: <CheckIcon size={14} /> },
  failing: { tone: "failing", text: "Pair check failing", icon: <CrossIcon size={14} /> },
  merge_conflict: { tone: "failing", text: "Merge conflict", icon: <CrossIcon size={14} /> },
  incomplete: { tone: "warning", text: "Pair check incomplete", icon: <CircleIcon size={14} /> },
};

/** State colour always comes with an icon and a text label (requirement 4.2). */
function Badge({ tone, text, icon }: { tone: Tone; text: string; icon: ReactNode }) {
  return (
    <span className={`badge badge-${tone}`}>
      {icon}
      {text}
    </span>
  );
}

export const CollisionBadge = ({ state }: { state: CollisionState }) => (
  <Badge {...COLLISION_STATES[state]} />
);
export const EvidenceBadge = ({ state }: { state: EvidenceState }) => (
  <Badge {...EVIDENCE_STATES[state]} />
);
export const OutcomeBadge = ({ outcome }: { outcome: CombinationOutcome }) => (
  <Badge {...OUTCOMES[outcome]} />
);

export function LiveDot({ state }: { state: "live" | "connecting" | "delayed" }) {
  const text = state === "live" ? "Live" : state === "connecting" ? "Connecting…" : "Delayed";
  return (
    <span className={`live live-${state}`} role="status">
      {state === "live" ? <BoltIcon size={14} /> : <DotIcon size={12} />}
      {text}
    </span>
  );
}

export function Avatar({ human, size = 28 }: { human: Human | undefined; size?: number }) {
  const [failed, setFailed] = useState(false);
  const initial = human?.github_login.slice(0, 1).toUpperCase() ?? "?";
  if (!human || failed) {
    return (
      <span
        className="avatar avatar-fallback"
        style={{ width: size, height: size }}
        aria-hidden="true"
      >
        {initial}
      </span>
    );
  }
  return (
    <img
      className="avatar"
      src={`https://avatars.githubusercontent.com/u/${human.github_user_id}?s=${size * 2}&v=4`}
      width={size}
      height={size}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  );
}

/**
 * Identifiers stay off the page except on Account and behind this explicit
 * "Copy ID" action (requirement 5.2).
 */
export function CopyIdMenu({ id, what }: { id: string; what: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className="icon-button" aria-label={`More actions for ${what}`}>
        <MoreIcon />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="menu" align="end" sideOffset={4}>
          <DropdownMenu.Item
            className="menu-item"
            onSelect={() => {
              void navigator.clipboard?.writeText(id).then(() => setCopied(true));
            }}
          >
            {copied ? "Copied" : "Copy ID"}
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

export function EmptyState({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="empty">
      <p className="empty-title">{title}</p>
      <div className="empty-body">{children}</div>
    </div>
  );
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

export function formatRelative(timestamp: number | string, now = Date.now()): string {
  const then = typeof timestamp === "number" ? timestamp : Date.parse(timestamp);
  const seconds = Math.round((then - now) / 1000);
  const abs = Math.abs(seconds);
  if (abs < 60) return relative.format(seconds, "second");
  if (abs < 3600) return relative.format(Math.round(seconds / 60), "minute");
  if (abs < 86_400) return relative.format(Math.round(seconds / 3600), "hour");
  return relative.format(Math.round(seconds / 86_400), "day");
}

/** Re-renders every few seconds so relative times stay true. */
export function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return now;
}

export function RelativeTime({ value }: { value: number | string }) {
  const now = useNow();
  const date = new Date(value);
  return (
    <time dateTime={date.toISOString()} title={date.toLocaleString()}>
      {formatRelative(value, now)}
    </time>
  );
}
