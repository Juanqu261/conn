import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type ReactNode, useEffect, useState } from "react";
import { projectsQuery } from "../api/queries";
import type { RepositoryConnection, Role, UUID } from "../api/types";
import { CopyIcon } from "../components/icons";
import { projectIdFromSlug } from "../names/directory";
import { useSession } from "../session";

/** The Project of the current /p/$project route. */
export function useProjectId(): UUID {
  const { project } = useParams({ from: "/p/$project" });
  return projectIdFromSlug(project);
}

export function useProjectSlug(): string {
  return useParams({ from: "/p/$project" }).project;
}

/** The signed-in Human and their role in the Project. */
export function useViewer(projectId: UUID): { me: UUID | undefined; role: Role | undefined } {
  const { session } = useSession();
  const projects = useQuery(projectsQuery);
  return {
    me: session.data?.human.human_id,
    role: projects.data?.projects.find((entry) => entry.project.project_id === projectId)?.role,
  };
}

/** Pending and error states shared by every read. */
export function QueryState({
  query,
  what,
  children,
}: {
  query: { isPending: boolean; isError: boolean; error: Error | null; refetch(): unknown };
  what: string;
  children: () => ReactNode;
}) {
  if (query.isPending) {
    return (
      <p className="muted" role="status">
        Reading {what}…
      </p>
    );
  }
  if (query.isError) {
    return (
      <div className="notice notice-failing" role="alert">
        <p>{query.error?.message ?? `Conn could not read ${what}.`}</p>
        <button
          type="button"
          className="button button-secondary"
          onClick={() => void query.refetch()}
        >
          Try again
        </button>
      </div>
    );
  }
  return <>{children()}</>;
}

/**
 * Untrusted Markdown from Conn records, shown as plain text with its line
 * breaks. No Markdown is ever turned into HTML, so nothing in a record can
 * inject markup.
 */
export function MarkdownText({ text, className }: { text: string; className?: string }) {
  return <div className={["prose-text", className].filter(Boolean).join(" ")}>{text}</div>;
}

/** "View on GitHub" for a GitHub remote and branch; undefined otherwise. Conn makes no GitHub call. */
export function githubTreeUrl(
  repository: RepositoryConnection | undefined,
  branch: string | undefined,
): string | undefined {
  if (repository?.host_kind !== "github" || !branch) return undefined;
  const [host, ...path] = repository.canonical_remote.split("/");
  if (host !== "github.com" || path.length !== 2) return undefined;
  const encodedBranch = branch.split("/").map(encodeURIComponent).join("/");
  return `https://github.com/${path.map(encodeURIComponent).join("/")}/tree/${encodedBranch}`;
}

export const shortCommit = (commit: string | undefined) => (commit ? commit.slice(0, 7) : "");

/** A copy button that confirms briefly; the text never leaves the browser. */
export function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <button
      type="button"
      className="button button-secondary button-small"
      onClick={() => void navigator.clipboard?.writeText(text).then(() => setCopied(true))}
    >
      <CopyIcon size={14} /> {copied ? "Copied" : label}
      <span className="sr-only" role="status">
        {copied ? "Copied to the clipboard" : ""}
      </span>
    </button>
  );
}

export const SEVERITY_LABELS = { info: "Info", amber: "Amber", red: "Red" } as const;
