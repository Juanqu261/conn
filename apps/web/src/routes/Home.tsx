import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { projectsQuery } from "../api/queries";
import { useDocumentTitle } from "../components/title";
import { EmptyState } from "../components/ui";
import { projectSlug } from "../names/directory";
import { lastProject } from "../theme/preferences";

/**
 * "/" opens the last Project this browser used, or the only one; otherwise
 * it lists the signed-in Human's Projects.
 */
export function Home() {
  const projects = useQuery(projectsQuery);
  const navigate = useNavigate();
  useDocumentTitle("Projects");

  const list = projects.data?.projects ?? [];
  const remembered = lastProject();
  const target =
    list.find((entry) => entry.project.project_id === remembered) ??
    (list.length === 1 ? list[0] : undefined);

  useEffect(() => {
    if (target) {
      void navigate({
        to: "/p/$project",
        params: { project: projectSlug(target.project.project_id, target.project.name) },
        replace: true,
      });
    }
  }, [target, navigate]);

  if (projects.isPending || target) {
    return (
      <div className="page" role="status">
        <p className="muted">Finding your Projects…</p>
      </div>
    );
  }
  if (projects.isError) {
    return (
      <div className="page">
        <div className="notice notice-failing" role="alert">
          {projects.error.message}
        </div>
      </div>
    );
  }
  return (
    <div className="page page-narrow">
      <header className="page-header">
        <h1 className="page-title">Your Projects</h1>
      </header>
      {list.length === 0 ? (
        <EmptyState title="No Projects in the Lab yet.">
          Create one in <code className="mono">conn app</code>, or ask a Project owner to add you by
          the Conn ID on your <Link to="/account">Account</Link> page.
        </EmptyState>
      ) : (
        <ul className="list">
          {list.map(({ project, role }) => (
            <li key={project.project_id} className="row">
              <div className="row-main">
                <Link
                  className="row-title row-link"
                  to="/p/$project"
                  params={{ project: projectSlug(project.project_id, project.name) }}
                >
                  {project.name}
                </Link>
                <p className="row-meta">You are {role === "owner" ? "the owner" : "a member"}</p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
