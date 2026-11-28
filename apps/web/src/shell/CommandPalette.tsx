import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Command } from "cmdk";
import { projectsQuery } from "../api/queries";
import type { UUID } from "../api/types";
import { LABELS } from "../labels";
import { projectSlug } from "../names/directory";
import { useProjectData } from "../project";
import { useSession } from "../session";
import { effectiveTheme, setPlainLabels, setTheme, usePlainLabels } from "../theme/preferences";

/**
 * Jump to any view, Project, Objective, Intent, or collision by name
 * (requirement 3.3). Records open on the overview, focused; their own pages
 * arrive with web-lab-board.
 */
export function CommandPalette({
  open,
  onOpenChange,
  projectId,
  onHelp,
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
  projectId: UUID | undefined;
  onHelp(): void;
}) {
  const navigate = useNavigate();
  const projects = useQuery(projectsQuery);
  const { radar, names } = useProjectData(projectId);
  const { signOut } = useSession();
  const plain = usePlainLabels();
  const project = projects.data?.projects.find(
    (entry) => entry.project.project_id === projectId,
  )?.project;
  const slug = project ? projectSlug(project.project_id, project.name) : undefined;

  const run = (action: () => void) => {
    onOpenChange(false);
    action();
  };
  const focus = (kind: string, id: UUID) => {
    if (slug) {
      void navigate({
        to: "/p/$project",
        params: { project: slug },
        search: { focus: `${kind}:${id}` },
      });
    }
  };

  return (
    <Command.Dialog
      open={open}
      onOpenChange={onOpenChange}
      label="Search the Lab"
      className="palette"
      overlayClassName="palette-overlay"
      contentClassName="palette-content"
    >
      <Command.Input
        className="palette-input"
        placeholder="Jump to a view, Project, Objective, Intent, or stitch…"
      />
      <Command.List className="palette-list">
        <Command.Empty className="palette-empty">Nothing by that name in the Lab.</Command.Empty>

        <Command.Group heading="Views" className="palette-group">
          {slug && (
            <>
              <Command.Item
                className="palette-item"
                onSelect={() =>
                  run(() => void navigate({ to: "/p/$project", params: { project: slug } }))
                }
              >
                {LABELS.lab.themed} <span className="palette-hint">{LABELS.lab.plain} · g o</span>
              </Command.Item>
              <Command.Item
                className="palette-item"
                onSelect={() =>
                  run(() => void navigate({ to: "/p/$project/feed", params: { project: slug } }))
                }
              >
                {LABELS.feed.themed} <span className="palette-hint">{LABELS.feed.plain} · g f</span>
              </Command.Item>
            </>
          )}
          <Command.Item
            className="palette-item"
            onSelect={() => run(() => void navigate({ to: "/account" }))}
          >
            Account <span className="palette-hint">g a</span>
          </Command.Item>
          <Command.Item
            className="palette-item"
            onSelect={() => run(() => void navigate({ to: "/" }))}
          >
            All Projects
          </Command.Item>
        </Command.Group>

        {(projects.data?.projects.length ?? 0) > 1 && (
          <Command.Group heading="Switch Project" className="palette-group">
            {projects.data?.projects.map(({ project: p }) => (
              <Command.Item
                key={p.project_id}
                value={`project ${p.name} ${p.project_id}`}
                className="palette-item"
                onSelect={() =>
                  run(
                    () =>
                      void navigate({
                        to: "/p/$project",
                        params: { project: projectSlug(p.project_id, p.name) },
                      }),
                  )
                }
              >
                {p.name}
              </Command.Item>
            ))}
          </Command.Group>
        )}

        {radar.data && radar.data.objectives.length > 0 && (
          <Command.Group heading="Objectives" className="palette-group">
            {radar.data.objectives.map((objective) => (
              <Command.Item
                key={objective.objective_id}
                value={`objective ${objective.title} ${objective.objective_id}`}
                className="palette-item"
                onSelect={() => run(() => focus("objective", objective.objective_id))}
              >
                {objective.title}
              </Command.Item>
            ))}
          </Command.Group>
        )}

        {radar.data && radar.data.active_intents.length > 0 && (
          <Command.Group heading="Intents" className="palette-group">
            {radar.data.active_intents.map((intent) => (
              <Command.Item
                key={intent.intent_id}
                value={`intent ${names.intent(intent.intent_id)} ${names.human(names.intentOwner(intent.intent_id))} ${intent.intent_id}`}
                className="palette-item"
                onSelect={() => run(() => focus("intent", intent.intent_id))}
              >
                {names.intent(intent.intent_id)}
                <span className="palette-hint">
                  {names.human(names.intentOwner(intent.intent_id))}
                </span>
              </Command.Item>
            ))}
          </Command.Group>
        )}

        {radar.data && radar.data.open_collisions.length > 0 && (
          <Command.Group heading="Stitches" className="palette-group">
            {radar.data.open_collisions.map((collision) => (
              <Command.Item
                key={collision.collision_id}
                value={`stitch collision ${names.collisionName(collision)} ${collision.collision_id}`}
                className="palette-item"
                onSelect={() => run(() => focus("collision", collision.collision_id))}
              >
                {names.collisionName(collision)}
              </Command.Item>
            ))}
          </Command.Group>
        )}

        <Command.Group heading="Actions" className="palette-group">
          <Command.Item
            className="palette-item"
            onSelect={() => run(() => setTheme(effectiveTheme() === "dark" ? "light" : "dark"))}
          >
            {effectiveTheme() === "dark" ? "Lights on (light theme)" : "Lights out (dark theme)"}
          </Command.Item>
          <Command.Item className="palette-item" onSelect={() => run(() => setPlainLabels(!plain))}>
            {plain ? "Show themed labels" : "Use plain labels"}
          </Command.Item>
          <Command.Item className="palette-item" onSelect={() => run(onHelp)}>
            Keyboard shortcuts <span className="palette-hint">?</span>
          </Command.Item>
          <Command.Item className="palette-item" onSelect={() => run(() => void signOut())}>
            Sign out
          </Command.Item>
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  );
}
