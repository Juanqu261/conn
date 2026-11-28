import { Outlet, useParams } from "@tanstack/react-router";
import { useEffect } from "react";
import { projectIdFromSlug } from "../names/directory";
import { rememberProject } from "../theme/preferences";

/** A Project's views. The Project ID in the URL is remembered per browser. */
export function ProjectLayout() {
  const { project } = useParams({ from: "/p/$project" });
  const projectId = projectIdFromSlug(project);
  useEffect(() => {
    rememberProject(projectId);
  }, [projectId]);
  return <Outlet />;
}
