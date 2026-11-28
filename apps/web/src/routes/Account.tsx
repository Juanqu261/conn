import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { projectsQuery, sessionQuery } from "../api/queries";
import { useDocumentTitle } from "../components/title";
import { Avatar } from "../components/ui";
import { useSession } from "../session";
import {
  lastProject,
  setPlainLabels,
  setTheme,
  type ThemeChoice,
  usePlainLabels,
  useTheme,
} from "../theme/preferences";

/**
 * The one page that shows identifiers (requirement 5.2): a Project owner
 * adds members by Conn human ID, so the Human needs a way to share it.
 */
export function Account() {
  const session = useQuery(sessionQuery);
  const projects = useQuery(projectsQuery);
  const { signOut } = useSession();
  const theme = useTheme();
  const plain = usePlainLabels();
  useDocumentTitle("Account");

  const human = session.data?.human;
  const remembered = lastProject();
  const project = projects.data?.projects.find(
    (entry) => entry.project.project_id === remembered,
  )?.project;

  return (
    <div className="page page-narrow">
      <header className="page-header account-header">
        <Avatar human={human} size={56} />
        <div>
          <h1 className="page-title">@{human?.github_login}</h1>
          {session.data && (
            <p className="muted">
              Signed in to Conn Lab until {new Date(session.data.expires_at).toLocaleString()}
            </p>
          )}
        </div>
      </header>

      <section className="section" aria-labelledby="ids-heading">
        <h2 id="ids-heading" className="section-title">
          Identifiers
        </h2>
        <p className="muted">Share your Conn ID with a Project owner so they can add you.</p>
        <dl className="facts">
          {human && <IdFact label="Your Conn ID" value={human.human_id} />}
          {project && <IdFact label={`Project ID of ${project.name}`} value={project.project_id} />}
        </dl>
      </section>

      <section className="section" aria-labelledby="prefs-heading">
        <h2 id="prefs-heading" className="section-title">
          This browser
        </h2>
        <fieldset className="choice-group">
          <legend>Theme</legend>
          {(
            [
              ["system", "Match the system"],
              ["dark", "Lights out (dark)"],
              ["light", "Lights on (light)"],
            ] as Array<[ThemeChoice, string]>
          ).map(([value, text]) => (
            <label key={value} className="choice">
              <input
                type="radio"
                name="theme"
                value={value}
                checked={theme === value}
                onChange={() => setTheme(value)}
              />
              {text}
            </label>
          ))}
        </fieldset>
        <label className="choice">
          <input
            type="checkbox"
            checked={plain}
            onChange={(event) => setPlainLabels(event.target.checked)}
          />
          Plain labels: show “Overview” instead of “The Lab”, and so on
        </label>
      </section>

      <section className="section" aria-labelledby="session-heading">
        <h2 id="session-heading" className="section-title">
          Session
        </h2>
        <p className="muted">
          Signing out ends this browser session on the server. Your{" "}
          <code className="mono">conn login</code> session in the terminal is separate and stays
          signed in.
        </p>
        <button type="button" className="button button-secondary" onClick={() => void signOut()}>
          Sign out of Conn Lab
        </button>
      </section>
    </div>
  );
}

function IdFact({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <div className="fact">
      <dt>{label}</dt>
      <dd>
        <code className="mono">{value}</code>
        <button
          type="button"
          className="button button-small"
          onClick={() => void navigator.clipboard?.writeText(value).then(() => setCopied(true))}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </dd>
    </div>
  );
}
