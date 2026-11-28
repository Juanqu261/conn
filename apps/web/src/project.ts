import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { intentsQuery, membersQuery, projectsQuery, radarQuery } from "./api/queries";
import type { UUID } from "./api/types";
import { buildNames } from "./names/directory";

/** The read models of one Project and the name directory built from them. */
export function useProjectData(projectId: UUID | undefined) {
  const enabled = Boolean(projectId);
  const id = projectId ?? "";
  const projects = useQuery(projectsQuery);
  const radar = useQuery({ ...radarQuery(id), enabled });
  const intents = useQuery({ ...intentsQuery(id), enabled });
  const members = useQuery({ ...membersQuery(id), enabled });

  const names = useMemo(
    () =>
      buildNames({
        projects: projects.data,
        radar: radar.data,
        intents: intents.data,
        members: members.data,
      }),
    [projects.data, radar.data, intents.data, members.data],
  );
  return { projects, radar, intents, members, names };
}
