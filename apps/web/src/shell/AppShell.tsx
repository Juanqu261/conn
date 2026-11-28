import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { DropdownMenu } from "radix-ui";
import { type ReactNode, useState } from "react";
import { projectsQuery } from "../api/queries";
import type { Human, UUID } from "../api/types";
import {
  BoltIcon,
  ChevronIcon,
  FlaskIcon,
  KeyboardIcon,
  PersonIcon,
  SearchIcon,
} from "../components/icons";
import { Avatar } from "../components/ui";
import { LABELS } from "../labels";
import { projectIdFromSlug, projectSlug } from "../names/directory";
import { useSession } from "../session";
import { ProjectStreamProvider } from "../stream/ProjectStream";
import {
  lastProject,
  setPlainLabels,
  setTheme,
  type ThemeChoice,
  usePlainLabels,
  useTheme,
} from "../theme/preferences";
import { CommandPalette } from "./CommandPalette";
import { FeedPanel } from "./FeedPanel";
import { HelpSheet } from "./HelpSheet";
import { useShortcuts } from "./shortcuts";
import { PROJECT_VIEWS, type ProjectPath } from "./views";

/**
 * The Lab's frame: top bar, navigation rail (a bottom bar on narrow
 * screens), the page, and the Lightning feed beside it on wide screens.
 * Later milestones add their views to the rail; none are shown before then.
 */
export function AppShell({ human, children }: { human: Human; children: ReactNode }) {
  const params = useParams({ strict: false }) as { project?: string };
  const projects = useQuery(projectsQuery);
  const navigate = useNavigate();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);

  // On pages outside a Project (Account, the Project list), the rail and
  // feed follow the last Project this browser opened, if still visible.
  const remembered = lastProject();
  const projectId: UUID | undefined = params.project
    ? projectIdFromSlug(params.project)
    : projects.data?.projects.find((entry) => entry.project.project_id === remembered)?.project
        .project_id;
  const project = projects.data?.projects.find(
    (entry) => entry.project.project_id === projectId,
  )?.project;
  const slug = project ? projectSlug(project.project_id, project.name) : params.project;

  useShortcuts({
    palette: () => setPaletteOpen(true),
    help: () => setHelpOpen(true),
    go: (key) => {
      const view = PROJECT_VIEWS.find((item) => item.key === key);
      if (key === "a") {
        void navigate({ to: "/account" });
      } else if (slug && view) {
        void navigate({ to: view.to, params: { project: slug } });
      } else {
        void navigate({ to: "/" });
      }
    },
  });

  const frame = (
    <div className={projectId ? "shell has-feed" : "shell"}>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="topbar">
        <Link to="/" className="brand" aria-label="Conn Lab home">
          <span className="brand-mark" aria-hidden="true">
            <BoltIcon size={18} />
          </span>
          <span className="wordmark" aria-hidden="true">
            conn lab
          </span>
        </Link>
        <ProjectSwitcher currentId={projectId} />
        <button type="button" className="search-button" onClick={() => setPaletteOpen(true)}>
          <SearchIcon size={16} />
          <span className="search-label">Search the Lab</span>
          <kbd className="kbd">Ctrl K</kbd>
        </button>
        <AccountMenu human={human} onHelp={() => setHelpOpen(true)} />
      </header>

      <nav className="rail" aria-label="Lab views">
        {slug ? (
          PROJECT_VIEWS.map((view) => (
            <RailLink
              key={view.to}
              to={view.to}
              slug={slug}
              icon={view.icon}
              text={view.text}
              title={view.label.plain}
              exact={Boolean(view.exact)}
            />
          ))
        ) : (
          <RailLink to="/" icon={<FlaskIcon />} text="Lab" title={LABELS.lab.plain} exact />
        )}
        <RailLink to="/account" icon={<PersonIcon />} text="Account" title="Account" />
      </nav>

      <main id="main" className="main" tabIndex={-1}>
        {children}
      </main>

      {projectId && (
        <aside className="feed-panel" aria-label={LABELS.feed.plain}>
          <FeedPanel projectId={projectId} compact />
        </aside>
      )}

      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        projectId={projectId}
        onHelp={() => setHelpOpen(true)}
      />
      <HelpSheet open={helpOpen} onOpenChange={setHelpOpen} />
    </div>
  );

  return projectId ? (
    <ProjectStreamProvider key={projectId} projectId={projectId}>
      {frame}
    </ProjectStreamProvider>
  ) : (
    frame
  );
}

function RailLink({
  to,
  slug,
  icon,
  text,
  title,
  exact,
}: {
  to: "/" | ProjectPath | "/account";
  slug?: string | undefined;
  icon: ReactNode;
  text: string;
  title: string;
  exact?: boolean;
}) {
  const link =
    to !== "/" && to !== "/account" ? (
      <Link
        to={to}
        params={{ project: slug ?? "" }}
        className="rail-link"
        activeOptions={{ exact: Boolean(exact) }}
        activeProps={{ "aria-current": "page" }}
        title={title}
      >
        {icon}
        <span>{text}</span>
      </Link>
    ) : (
      <Link
        to={to}
        className="rail-link"
        activeOptions={{ exact: true }}
        activeProps={{ "aria-current": "page" }}
        title={title}
      >
        {icon}
        <span>{text}</span>
      </Link>
    );
  return link;
}

function ProjectSwitcher({ currentId }: { currentId: UUID | undefined }) {
  const projects = useQuery(projectsQuery);
  const navigate = useNavigate();
  const current = projects.data?.projects.find(
    (entry) => entry.project.project_id === currentId,
  )?.project;
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className="switcher-trigger">
        <span className="switcher-name">{current?.name ?? "Choose a Project"}</span>
        <ChevronIcon size={16} />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="menu" align="start" sideOffset={6}>
          <DropdownMenu.Label className="menu-label">Your Projects</DropdownMenu.Label>
          {projects.data?.projects.length === 0 && (
            <div className="menu-note">You are not in any Project yet.</div>
          )}
          <DropdownMenu.RadioGroup value={currentId ?? ""}>
            {projects.data?.projects.map(({ project, role }) => (
              <DropdownMenu.RadioItem
                key={project.project_id}
                value={project.project_id}
                className="menu-item"
                onSelect={() =>
                  void navigate({
                    to: "/p/$project",
                    params: { project: projectSlug(project.project_id, project.name) },
                  })
                }
              >
                <span className="menu-item-text">{project.name}</span>
                <span className="menu-item-hint">{role}</span>
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className="menu-separator" />
          <DropdownMenu.Item className="menu-item" onSelect={() => void navigate({ to: "/" })}>
            All Projects
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

const THEMES: Array<{ value: ThemeChoice; text: string }> = [
  { value: "system", text: "Match the system" },
  { value: "dark", text: "Lights out (dark)" },
  { value: "light", text: "Lights on (light)" },
];

function AccountMenu({ human, onHelp }: { human: Human; onHelp(): void }) {
  const { signOut } = useSession();
  const navigate = useNavigate();
  const theme = useTheme();
  const plain = usePlainLabels();
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger
        className="account-trigger"
        aria-label={`Account menu for @${human.github_login}`}
      >
        <Avatar human={human} size={30} />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="menu" align="end" sideOffset={6}>
          <DropdownMenu.Label className="menu-label">@{human.github_login}</DropdownMenu.Label>
          <DropdownMenu.Item
            className="menu-item"
            onSelect={() => void navigate({ to: "/account" })}
          >
            Account
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="menu-separator" />
          <DropdownMenu.Label className="menu-label">Theme</DropdownMenu.Label>
          <DropdownMenu.RadioGroup
            value={theme}
            onValueChange={(value) => setTheme(value as ThemeChoice)}
          >
            {THEMES.map((option) => (
              <DropdownMenu.RadioItem
                key={option.value}
                value={option.value}
                className="menu-item menu-radio"
              >
                <DropdownMenu.ItemIndicator className="menu-indicator">
                  ●
                </DropdownMenu.ItemIndicator>
                {option.text}
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.CheckboxItem
            className="menu-item menu-radio"
            checked={plain}
            onCheckedChange={(checked) => setPlainLabels(checked === true)}
          >
            <DropdownMenu.ItemIndicator className="menu-indicator">✓</DropdownMenu.ItemIndicator>
            Plain labels
          </DropdownMenu.CheckboxItem>
          <DropdownMenu.Separator className="menu-separator" />
          <DropdownMenu.Item className="menu-item" onSelect={onHelp}>
            <KeyboardIcon size={16} /> Keyboard shortcuts
          </DropdownMenu.Item>
          <DropdownMenu.Item className="menu-item" onSelect={() => void signOut()}>
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
