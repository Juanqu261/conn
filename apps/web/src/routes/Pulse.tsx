import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { insightsQuery } from "../api/queries";
import type { Median, ProjectInsights, Ratio } from "../api/types";
import { useDocumentTitle } from "../components/title";
import { ThemedLabel } from "../components/ui";
import { LABELS } from "../labels";
import { RULE_LABELS } from "../names/directory";
import { KIND_LABELS } from "../stitches/decision";
import { QueryState, useProjectId } from "./common";

export const WINDOWS = [7, 30, 90] as const;
export type PulseWindow = (typeof WINDOWS)[number];

const OUTCOMES: Record<string, string> = {
  passing: "Passing",
  failing: "Failing",
  merge_conflict: "Merge conflict",
  incomplete: "Incomplete",
};

interface Datum {
  label: string;
  value: number;
}

/**
 * Pulse: Project insights as charts (requirement 10). Every measure is
 * Project-level; nothing here names, counts, or filters by a person. Small
 * samples show "Not enough data yet" and nothing is extrapolated.
 */
export function Pulse() {
  const projectId = useProjectId();
  const search = useSearch({ from: "/p/$project/pulse" });
  const navigate = useNavigate({ from: "/p/$project/pulse" });
  const days = search.window ?? 30;
  const insights = useQuery(insightsQuery(projectId, days));
  useDocumentTitle(LABELS.pulse.themed);
  return (
    <div className="page">
      <header className="page-header">
        <ThemedLabel label={LABELS.pulse} as="h1" className="page-title" />
        <fieldset className="segmented">
          <legend className="sr-only">Window</legend>
          {WINDOWS.map((value) => (
            <button
              key={value}
              type="button"
              className="chip"
              aria-pressed={value === days}
              onClick={() =>
                void navigate({
                  search: { window: value === 30 ? undefined : value },
                  replace: true,
                })
              }
            >
              {value} days
            </button>
          ))}
        </fieldset>
      </header>
      <QueryState query={insights} what="the insights">
        {() => (insights.data ? <Charts data={insights.data} /> : null)}
      </QueryState>
    </div>
  );
}

function Charts({ data }: { data: ProjectInsights }) {
  const minimum = data.minimum_denominator;
  const range = `${new Date(data.window_start).toLocaleDateString()} to ${new Date(data.window_end).toLocaleDateString()}`;
  const decisions = new Map<string, number>();
  for (const item of data.decisions)
    decisions.set(item.kind, (decisions.get(item.kind) ?? 0) + item.count);
  const falsePositives = data.false_positive_share.reduce(
    (sum, item) => ({
      numerator: sum.numerator + item.share.numerator,
      denominator: sum.denominator + item.share.denominator,
    }),
    { numerator: 0, denominator: 0 },
  );
  const share: Ratio = {
    ...falsePositives,
    status: falsePositives.denominator >= minimum ? "value" : "insufficient_data",
    ...(falsePositives.denominator > 0
      ? { value: falsePositives.numerator / falsePositives.denominator }
      : {}),
  };
  return (
    <>
      <p className="as-of">
        The last {data.since_days} days ({range}). Measures with fewer than {minimum} samples show
        no chart.
      </p>
      <div className="pulse-grid">
        <Chart
          title="Collisions opened, by rule"
          samples={sum(data.collisions_opened.map((i) => i.count))}
          minimum={minimum}
        >
          <Bars
            data={data.collisions_opened.map((item) => ({
              label: RULE_LABELS[item.rule_id] ?? item.rule_id,
              value: item.count,
            }))}
            unit="collisions"
          />
        </Chart>
        <Chart title="Decisions, by kind" samples={sum([...decisions.values()])} minimum={minimum}>
          <Bars
            data={[...decisions].map(([kind, value]) => ({
              label: KIND_LABELS[kind as keyof typeof KIND_LABELS] ?? kind,
              value,
            }))}
            unit="decisions"
          />
        </Chart>
        <Chart
          title="False-positive share of decisions"
          samples={share.denominator}
          minimum={minimum}
        >
          <Gauge ratio={share} what="decisions were false positives" />
        </Chart>
        <Chart
          title="Median hours to the first decision"
          samples={data.median_hours_to_first_decision.sample_size}
          minimum={minimum}
        >
          <Figure median={data.median_hours_to_first_decision} days={data.since_days} />
        </Chart>
        <Chart
          title="First decisions made before release"
          samples={data.decided_before_release.denominator}
          minimum={minimum}
        >
          <Gauge
            ratio={data.decided_before_release}
            what="first decisions came before either Intent was released"
          />
        </Chart>
        <Chart
          title="Analysed revisions with undeclared changes"
          samples={data.undeclared_observed_changes.denominator}
          minimum={minimum}
        >
          <Gauge
            ratio={data.undeclared_observed_changes}
            what="analysed revisions changed subjects nobody declared"
          />
        </Chart>
        <Chart
          title="Acceptance criteria, by provenance"
          samples={
            data.acceptance_by_provenance.observed_run +
            data.acceptance_by_provenance.reported +
            data.acceptance_by_provenance.missing
          }
          minimum={minimum}
        >
          <Stacked
            data={[
              { label: "Observed", value: data.acceptance_by_provenance.observed_run },
              { label: "Reported", value: data.acceptance_by_provenance.reported },
              { label: "Missing", value: data.acceptance_by_provenance.missing },
            ]}
          />
        </Chart>
        <Chart
          title="Pair checks, by outcome"
          samples={sum(data.combination_checks.map((i) => i.count))}
          minimum={minimum}
        >
          <Bars
            data={data.combination_checks.map((item) => ({
              label: OUTCOMES[item.outcome] ?? item.outcome,
              value: item.count,
            }))}
            unit="checks"
          />
        </Chart>
      </div>
    </>
  );
}

const sum = (values: number[]) => values.reduce((total, value) => total + value, 0);

function Chart({
  title,
  samples,
  minimum,
  children,
}: {
  title: string;
  samples: number;
  minimum: number;
  children: ReactNode;
}) {
  return (
    <section className="chart" aria-label={title}>
      <h2 className="chart-title">{title}</h2>
      {samples < minimum ? (
        <p className="chart-empty">
          Not enough data yet{" "}
          <span className="row-meta">
            ({samples} of {minimum} samples)
          </span>
        </p>
      ) : (
        children
      )}
    </section>
  );
}

/** A horizontal bar chart in SVG with its table alternative. */
function Bars({ data, unit }: { data: Datum[]; unit: string }) {
  const max = Math.max(1, ...data.map((d) => d.value));
  const row = 26;
  return (
    <>
      <svg
        className="chart-svg"
        viewBox={`0 0 320 ${data.length * row}`}
        role="img"
        aria-label={data.map((d) => `${d.label}: ${d.value} ${unit}`).join("; ")}
      >
        {data.map((d, i) => (
          <g key={d.label} transform={`translate(0 ${i * row})`}>
            <text x="0" y="16" className="chart-label">
              {d.label}
            </text>
            <rect
              x="150"
              y="5"
              width={Math.max(2, (d.value / max) * 140)}
              height="14"
              rx="3"
              className="chart-bar"
            />
            <text x={156 + (d.value / max) * 140} y="16" className="chart-value">
              {d.value}
            </text>
          </g>
        ))}
      </svg>
      <TableAlternative
        rows={data.map((d) => [d.label, String(d.value)])}
        head={["Category", unit]}
      />
    </>
  );
}

function Stacked({ data }: { data: Datum[] }) {
  const total = Math.max(1, sum(data.map((d) => d.value)));
  let x = 0;
  return (
    <>
      <svg
        className="chart-svg"
        viewBox="0 0 320 40"
        role="img"
        aria-label={data.map((d) => `${d.label}: ${d.value}`).join("; ")}
      >
        {data.map((d, i) => {
          const width = (d.value / total) * 320;
          const segment = (
            <rect
              key={d.label}
              x={x}
              y="4"
              width={width}
              height="20"
              className={`chart-segment chart-segment-${i}`}
            />
          );
          x += width;
          return segment;
        })}
      </svg>
      <ul className="chart-keys">
        {data.map((d, i) => (
          <li key={d.label}>
            <span className={`chart-key chart-segment-${i}`} aria-hidden="true" /> {d.label}{" "}
            {d.value}
          </li>
        ))}
      </ul>
      <TableAlternative
        rows={data.map((d) => [d.label, String(d.value)])}
        head={["Provenance", "Criteria"]}
      />
    </>
  );
}

function Gauge({ ratio, what }: { ratio: Ratio; what: string }) {
  if (ratio.status !== "value" || ratio.value === undefined) {
    return <p className="chart-empty">Not enough data yet</p>;
  }
  const percent = Math.round(ratio.value * 100);
  return (
    <>
      <svg
        className="chart-svg chart-gauge"
        viewBox="0 0 320 36"
        role="img"
        aria-label={`${percent}% (${ratio.numerator} of ${ratio.denominator}) ${what}`}
      >
        <rect x="0" y="8" width="320" height="16" rx="8" className="chart-track" />
        <rect
          x="0"
          y="8"
          width={Math.max(4, ratio.value * 320)}
          height="16"
          rx="8"
          className="chart-bar"
        />
      </svg>
      <p className="chart-figure">
        {percent}%{" "}
        <span className="row-meta">
          ({ratio.numerator} of {ratio.denominator})
        </span>
      </p>
      <TableAlternative
        rows={[
          ["Numerator", String(ratio.numerator)],
          ["Denominator", String(ratio.denominator)],
          ["Share", `${percent}%`],
        ]}
        head={["Measure", "Value"]}
      />
    </>
  );
}

function Figure({ median, days }: { median: Median; days: number }) {
  if (median.status !== "value" || median.value === undefined) {
    return <p className="chart-empty">Not enough data yet</p>;
  }
  return (
    <>
      <p className="chart-figure">
        {median.value.toFixed(1)} h{" "}
        <span className="row-meta">
          median over {median.sample_size} stitches in {days} days
        </span>
      </p>
      <TableAlternative
        rows={[
          ["Median hours", median.value.toFixed(1)],
          ["Samples", String(median.sample_size)],
        ]}
        head={["Measure", "Value"]}
      />
    </>
  );
}

function TableAlternative({ head, rows }: { head: [string, string]; rows: string[][] }) {
  return (
    <details className="chart-table">
      <summary>Show as a table</summary>
      <table className="data-table">
        <thead>
          <tr>
            <th scope="col">{head[0]}</th>
            <th scope="col">{head[1]}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row[0]}>
              <th scope="row">{row[0]}</th>
              <td>{row[1]}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}
