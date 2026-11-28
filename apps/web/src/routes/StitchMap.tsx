import { useNavigate, useSearch } from "@tanstack/react-router";
import type { Core, ElementDefinition, StylesheetJson } from "cytoscape";
import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import type { CollisionState } from "../api/types";
import { useDocumentTitle } from "../components/title";
import { CollisionBadge, EmptyState, OutcomeBadge, ThemedLabel } from "../components/ui";
import { LABELS } from "../labels";
import { buildMap, type MapModel, traversalOrder } from "../map/model";
import { type Names, RULE_LABELS } from "../names/directory";
import { useProjectData } from "../project";
import { QueryState, useProjectId, useProjectSlug } from "./common";

export interface MapSearch {
  view?: "graph" | "table" | undefined;
  subjects?: boolean | undefined;
  focus?: string | undefined;
}

const STATE_LEGEND: Array<{ state: CollisionState; line: string }> = [
  { state: "open", line: "dashed" },
  { state: "under_review", line: "dotted" },
  { state: "resolved", line: "solid" },
  { state: "dismissed", line: "faded" },
];

/**
 * The Stitch Map: the shape of concurrent work (requirement 8). Intents are
 * nodes clustered by Objective, collisions are edges styled by state. A
 * sorted list beside the graph makes it operable by keyboard, and the table
 * view lists the same stitches.
 */
export function StitchMap() {
  const projectId = useProjectId();
  const slug = useProjectSlug();
  const search = useSearch({ from: "/p/$project/map" });
  const navigate = useNavigate({ from: "/p/$project/map" });
  const { radar, names } = useProjectData(projectId);
  useDocumentTitle(LABELS.stitchMap.themed);
  const view = search.view ?? "graph";
  const model = useMemo(
    () =>
      radar.data
        ? buildMap(radar.data, names, { subjects: Boolean(search.subjects), focus: search.focus })
        : undefined,
    [radar.data, names, search.subjects, search.focus],
  );
  const set = (patch: Partial<MapSearch>) =>
    void navigate({ search: (s) => ({ ...s, ...patch }), replace: true });
  const openIntent = (intent: string) =>
    void navigate({ to: "/p/$project/intents/$intent", params: { project: slug, intent } });
  const openStitch = (collision: string) =>
    void navigate({ to: "/p/$project/stitches/$collision", params: { project: slug, collision } });

  return (
    <div className="page page-wide">
      <header className="page-header">
        <ThemedLabel label={LABELS.stitchMap} as="h1" className="page-title" />
        {radar.data && (
          <p className="as-of">As of event {radar.data.as_of_event.toLocaleString()}</p>
        )}
      </header>
      <div className="toolbar">
        <fieldset className="segmented">
          <legend className="sr-only">Map view</legend>
          {(["graph", "table"] as const).map((value) => (
            <button
              key={value}
              type="button"
              aria-pressed={view === value}
              className="chip"
              onClick={() => set({ view: value === "graph" ? undefined : value })}
            >
              {value === "graph" ? "Graph" : "Table"}
            </button>
          ))}
        </fieldset>
        <button
          type="button"
          className="chip"
          aria-pressed={Boolean(search.subjects)}
          onClick={() => set({ subjects: search.subjects ? undefined : true })}
        >
          Subject layer
        </button>
        {search.focus && (
          <button type="button" className="chip" onClick={() => set({ focus: undefined })}>
            Show everything (focused on {names.intent(search.focus)})
          </button>
        )}
      </div>
      <QueryState query={radar} what="the map">
        {() => {
          if (!model) return null;
          if (model.nodes.every((node) => node.kind !== "intent")) {
            return (
              <EmptyState title="No active work to map">
                Published Intents and their stitches appear here.
              </EmptyState>
            );
          }
          return (
            <>
              {model.omissions.length > 0 && (
                <div className="notice notice-warning" role="status">
                  <ul>
                    {model.omissions.map((text) => (
                      <li key={text}>{text}</li>
                    ))}
                  </ul>
                </div>
              )}
              {view === "graph" ? (
                <Graph
                  model={model}
                  names={names}
                  focus={search.focus}
                  onFocus={(focus) => set({ focus })}
                  onIntent={openIntent}
                  onStitch={openStitch}
                />
              ) : (
                <EdgeTable
                  model={model}
                  names={names}
                  onStitch={openStitch}
                  onIntent={openIntent}
                />
              )}
              <ul className="legend" aria-label="Edge legend">
                {STATE_LEGEND.map((item) => (
                  <li key={item.state}>
                    <span className={`legend-line legend-${item.state}`} aria-hidden="true" />{" "}
                    <CollisionBadge state={item.state} /> {item.line}
                  </li>
                ))}
                <li>
                  <span className="legend-marker" aria-hidden="true" /> failing pair check
                </li>
              </ul>
            </>
          );
        }}
      </QueryState>
    </div>
  );
}

function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || "#888888";
}

function styles(): StylesheetJson {
  const edgeColour: Record<string, string> = {
    open: token("--pumpkin"),
    under_review: token("--bruise"),
    resolved: token("--stitch"),
    dismissed: token("--ink-muted"),
  };
  return [
    {
      selector: "node[kind = 'intent']",
      style: {
        shape: "round-rectangle",
        "background-color": token("--crypt-raised"),
        "border-width": 2,
        "border-color": token("--stitch"),
        label: "data(label)",
        color: token("--ink"),
        "font-size": 11,
        "text-wrap": "ellipsis",
        "text-max-width": "140px",
        "text-valign": "bottom",
        "text-margin-y": 6,
        width: 44,
        height: 44,
      },
    },
    {
      selector: "node[avatar]",
      style: {
        "background-image": "data(avatar)",
        "background-fit": "cover",
        "background-image-crossorigin": "null",
      },
    },
    {
      selector: "node[kind = 'objective']",
      style: {
        shape: "round-rectangle",
        "background-color": token("--crypt-surface"),
        "background-opacity": 0.6,
        "border-width": 1,
        "border-style": "dashed",
        "border-color": token("--stitch"),
        label: "data(label)",
        color: token("--formal-ink"),
        "font-size": 12,
        "text-valign": "top",
        "text-halign": "center",
        padding: "18px",
      },
    },
    {
      selector: "node[kind = 'subject']",
      style: {
        shape: "ellipse",
        width: 10,
        height: 10,
        "background-color": token("--ink-muted"),
        label: "data(label)",
        "font-family": "JetBrains Mono Variable, monospace",
        "font-size": 9,
        color: token("--ink-muted"),
      },
    },
    { selector: "node:selected", style: { "border-color": token("--voltage"), "border-width": 4 } },
    {
      selector: "edge[kind = 'collision']",
      style: {
        width: 3,
        "curve-style": "bezier",
        "line-color": (edge: { data(key: string): string }) =>
          edgeColour[edge.data("state")] ?? token("--stitch"),
        "line-style": (edge: { data(key: string): string }) =>
          edge.data("state") === "open"
            ? "dashed"
            : edge.data("state") === "under_review"
              ? "dotted"
              : "solid",
        opacity: (edge: { data(key: string): string }) =>
          edge.data("state") === "dismissed" ? 0.3 : 1,
      },
    },
    {
      selector: "edge[failing]",
      style: {
        "mid-target-arrow-shape": "circle",
        "mid-target-arrow-color": token("--blood"),
        "arrow-scale": 1.4,
      },
    },
    {
      selector: "edge[kind = 'claim']",
      style: { width: 1, "line-color": token("--ink-muted"), opacity: 0.5 },
    },
    { selector: "edge:selected", style: { width: 5 } },
  ] as StylesheetJson;
}

function elements(model: MapModel, names: Names): ElementDefinition[] {
  const result: ElementDefinition[] = [];
  for (const node of model.nodes) {
    const data: Record<string, unknown> = { id: node.id, kind: node.kind, label: node.label };
    if (node.parent) data.parent = node.parent;
    const human = names.humanRecord(node.ownerHumanId);
    if (human)
      data.avatar = `https://avatars.githubusercontent.com/u/${human.github_user_id}?s=88&v=4`;
    result.push({ data });
  }
  for (const edge of model.edges) {
    const data: Record<string, unknown> = {
      id: edge.id,
      kind: edge.kind,
      source: edge.source,
      target: edge.target,
    };
    if (edge.collision) {
      data.state = edge.collision.state;
      data.collision = edge.collision.collision_id;
      const outcome = edge.collision.latest_combination_outcome;
      if (outcome === "failing" || outcome === "merge_conflict") data.failing = true;
    }
    result.push({ data });
  }
  return result;
}

function Graph({
  model,
  names,
  focus,
  onFocus,
  onIntent,
  onStitch,
}: {
  model: MapModel;
  names: Names;
  focus: string | undefined;
  onFocus(intent: string | undefined): void;
  onIntent(intent: string): void;
  onStitch(collision: string): void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const cy = useRef<Core | undefined>(undefined);
  const [failed, setFailed] = useState(false);
  const order = useMemo(() => traversalOrder(model, names), [model, names]);
  const [active, setActive] = useState(0);
  const handlers = useRef({ onIntent, onStitch });
  handlers.current = { onIntent, onStitch };

  useEffect(() => {
    let cancelled = false;
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
    void Promise.all([import("cytoscape"), import("cytoscape-fcose")])
      .then(([{ default: cytoscape }, { default: fcose }]) => {
        if (cancelled || !container.current) return;
        cytoscape.use(fcose);
        cy.current?.destroy();
        const instance = cytoscape({
          container: container.current,
          elements: elements(model, names),
          style: styles(),
          layout: {
            name: "fcose",
            animate: !reduced,
            randomize: false,
            nodeDimensionsIncludeLabels: true,
          } as never,
          wheelSensitivity: 0.3,
          minZoom: 0.2,
          maxZoom: 3,
        });
        instance.on("tap", "node[kind = 'intent']", (event) =>
          handlers.current.onIntent(String(event.target.id()).slice(7)),
        );
        instance.on("tap", "edge[kind = 'collision']", (event) =>
          handlers.current.onStitch(String(event.target.data("collision"))),
        );
        cy.current = instance;
      })
      .catch(() => setFailed(true));
    return () => {
      cancelled = true;
      cy.current?.destroy();
      cy.current = undefined;
    };
  }, [model, names]);

  // The keyboard list drives the same selection as the graph.
  useEffect(() => {
    const node = order[active];
    const instance = cy.current;
    if (!node || !instance) return;
    instance.elements().unselect();
    const element = instance.getElementById(node.id);
    element.select();
    instance.animate({ center: { eles: element } }, { duration: 0 });
  }, [active, order]);

  // A roving set of buttons: one tab stop, arrow keys move it, Enter opens
  // the Intent, and f focuses it with its neighbours.
  const items = useRef<Array<HTMLButtonElement | null>>([]);
  const move = (next: number) => {
    const index = Math.max(0, Math.min(order.length - 1, next));
    setActive(index);
    items.current[index]?.focus();
  };
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, i: number) => {
    if (event.key === "ArrowDown" || event.key === "ArrowRight") {
      event.preventDefault();
      move(i + 1);
    } else if (event.key === "ArrowUp" || event.key === "ArrowLeft") {
      event.preventDefault();
      move(i - 1);
    } else if (event.key === "Home") {
      event.preventDefault();
      move(0);
    } else if (event.key === "End") {
      event.preventDefault();
      move(order.length - 1);
    } else if (event.key === "f") {
      const node = order[i];
      if (node?.intentId) onFocus(node.intentId === focus ? undefined : node.intentId);
    }
  };

  return (
    <div className="map">
      <div
        className="map-canvas"
        ref={container}
        role="img"
        aria-label="Graph of active Intents and their stitches; use the list beside it or the table view to read it."
      >
        {failed && (
          <p className="notice notice-failing">
            The graph could not load. The table view lists every stitch.
          </p>
        )}
      </div>
      <div className="map-side">
        <div className="map-controls">
          <button
            type="button"
            className="button button-secondary button-small"
            onClick={() => cy.current?.zoom(cy.current.zoom() * 1.25)}
          >
            Zoom in
          </button>
          <button
            type="button"
            className="button button-secondary button-small"
            onClick={() => cy.current?.zoom(cy.current.zoom() / 1.25)}
          >
            Zoom out
          </button>
          <button
            type="button"
            className="button button-secondary button-small"
            onClick={() => cy.current?.fit(undefined, 24)}
          >
            Fit
          </button>
        </div>
        <p className="row-meta" id="map-list-help">
          Arrow keys move through Intents, Enter opens one, f focuses it and its neighbours.
        </p>
        <ul className="map-list" aria-label="Intents on the map" aria-describedby="map-list-help">
          {order.map((node, i) => (
            <li key={node.id}>
              <button
                type="button"
                ref={(element) => {
                  items.current[i] = element;
                }}
                tabIndex={i === active ? 0 : -1}
                aria-current={i === active ? "true" : undefined}
                className={i === active ? "map-item active" : "map-item"}
                onFocus={() => setActive(i)}
                onKeyDown={(event) => onKeyDown(event, i)}
                onClick={() => node.intentId && onIntent(node.intentId)}
              >
                {node.label}
                <span className="row-meta"> {names.human(node.ownerHumanId)}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function EdgeTable({
  model,
  names,
  onStitch,
  onIntent,
}: {
  model: MapModel;
  names: Names;
  onStitch(collision: string): void;
  onIntent(intent: string): void;
}) {
  const collisions = model.edges.flatMap((edge) => (edge.collision ? [edge.collision] : []));
  if (collisions.length === 0) {
    return (
      <EmptyState title="No stitches between the Intents shown">
        Collisions appear here when Conn finds them.
      </EmptyState>
    );
  }
  return (
    <div className="table-scroll">
      <table className="data-table">
        <caption className="sr-only">Stitches between active Intents</caption>
        <thead>
          <tr>
            <th scope="col">Stitch</th>
            <th scope="col">Intents</th>
            <th scope="col">State</th>
            <th scope="col">Matched subjects</th>
            <th scope="col">Pair check</th>
          </tr>
        </thead>
        <tbody>
          {collisions.map((collision) => (
            <tr key={collision.collision_id}>
              <td>
                <button
                  type="button"
                  className="link-button"
                  onClick={() => onStitch(collision.collision_id)}
                >
                  {RULE_LABELS[collision.rule_id] ?? collision.rule_id}
                </button>
              </td>
              <td>
                <button
                  type="button"
                  className="link-button"
                  onClick={() => onIntent(collision.left_intent_id)}
                >
                  {names.intent(collision.left_intent_id)}
                </button>{" "}
                ⇄{" "}
                <button
                  type="button"
                  className="link-button"
                  onClick={() => onIntent(collision.right_intent_id)}
                >
                  {names.intent(collision.right_intent_id)}
                </button>
              </td>
              <td>
                <CollisionBadge state={collision.state} />
              </td>
              <td className="mono">{collision.matched_subjects.join(", ")}</td>
              <td>
                {collision.latest_combination_outcome ? (
                  <OutcomeBadge outcome={collision.latest_combination_outcome} />
                ) : (
                  "Not run"
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
