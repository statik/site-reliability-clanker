import { Fragment, useState } from "react";
import {
  Dropdown,
  InlineNotification,
  Table,
  TableBody,
  TableCell,
  TableExpandedRow,
  TableExpandHeader,
  TableExpandRow,
  TableHead,
  TableHeader,
  TableRow,
  Tag,
} from "@carbon/react";
import type { Observation, ObservedComponent, ObservedInstance } from "@/api/types";
import { observationPath } from "@/api/urls";
import { Loaded, ScrollRegion, Section } from "@/components/common";
import { StatusTag } from "@/components/status";
import { anyEvent, useApi } from "@/hooks/useApi";
import { useSession } from "@/state/session";

/** The observer reports without audit events, so the panel also loads on this interval. */
const POLL_MS = 5000;
const NOT_FOUND = 404;
const NONE = "–";
const COLUMNS = ["Component", "State", "Ready", "Version", "Restarts"];

/** The version that all instances run, "mixed" when they differ, or a dash when none says. */
export function commonVersion(instances: ObservedInstance[]): string {
  const versions = new Set<string>();
  for (const instance of instances) {
    if (instance.version !== undefined && instance.version !== "") {
      versions.add(instance.version);
    }
  }
  if (versions.size > 1) {
    return "mixed";
  }
  return [...versions][0] ?? NONE;
}

function restartsText(restarts: number): string {
  if (restarts <= 0) {
    return NONE;
  }
  return restarts === 1 ? "1 restart" : `${restarts} restarts`;
}

function ReportAge({ observation }: { observation: Observation }) {
  if (observation.stale) {
    const title = observation.age_seconds < 0 ? "No report yet"
      : "No report for more than 60 seconds; the state is unknown";
    return (
      <InlineNotification kind="warning" lowContrast hideCloseButton className="app-note"
        title={title} />
    );
  }
  const age = observation.age_seconds;
  return (
    <p className="app-label">Last report {age} {age === 1 ? "second" : "seconds"} ago</p>
  );
}

function Instances({ component }: { component: ObservedComponent }) {
  if (component.instances.length === 0) {
    return <p className="app-secondary">No instance of {component.name} runs.</p>;
  }
  return (
    <Table size="sm" aria-label={`Instances of ${component.name}`}>
      <TableHead>
        <TableRow>
          <TableHeader>Instance</TableHeader>
          <TableHeader>State</TableHeader>
          <TableHeader>Version</TableHeader>
          <TableHeader>Restarts</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {component.instances.map((instance) => (
          <TableRow key={instance.id}>
            <TableCell><span className="app-mono">{instance.id}</span></TableCell>
            <TableCell><StatusTag status={instance.state} size="sm" /></TableCell>
            <TableCell>{commonVersion([instance])}</TableCell>
            <TableCell>{restartsText(instance.restarts)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function ComponentRow({ component }: { component: ObservedComponent }) {
  const [expanded, setExpanded] = useState(false);
  const restarts = component.instances.reduce((sum, instance) => sum + instance.restarts, 0);
  return (
    <Fragment>
      <TableExpandRow isExpanded={expanded} onExpand={() => setExpanded((open) => !open)}
        aria-label={`${expanded ? "Hide" : "Show"} the instances of ${component.name}`}>
        <TableCell>{component.name}</TableCell>
        <TableCell><StatusTag status={component.state} size="sm" /></TableCell>
        <TableCell>{component.ready} of {component.wanted} ready</TableCell>
        <TableCell>{commonVersion(component.instances)}</TableCell>
        <TableCell>{restartsText(restarts)}</TableCell>
      </TableExpandRow>
      {expanded && (
        <TableExpandedRow colSpan={COLUMNS.length + 1}>
          <Instances component={component} />
        </TableExpandedRow>
      )}
    </Fragment>
  );
}

/** One report of the observer: the age of the report and one row for each component. */
export function ComponentsView({ observation }: { observation: Observation }) {
  // An empty list is `null` in the JSON of some Go servers.
  const components = observation.components ?? [];
  const label = `Components of ${observation.application} in ${observation.env}`;
  return (
    <div className="app-stack-tight">
      <ReportAge observation={observation} />
      {components.length === 0 ? (
        <p className="app-secondary">The report names no component.</p>
      ) : (
        <ScrollRegion label={label}>
          <Table aria-label={label}>
            <TableHead>
              <TableRow>
                <TableExpandHeader aria-label="Instances" />
                {COLUMNS.map((column) => <TableHeader key={column}>{column}</TableHeader>)}
              </TableRow>
            </TableHead>
            <TableBody>
              {components.map((component) => (
                <ComponentRow key={component.name}
                  component={{ ...component, instances: component.instances ?? [] }} />
              ))}
            </TableBody>
          </Table>
        </ScrollRegion>
      )}
    </div>
  );
}

/**
 * The live state of the components of the application in one environment. It shows nothing for
 * a past time, because the report has no history, and nothing when the control plane does not
 * have the route.
 */
export function ComponentsPanel({ envs }: { envs: string[] }) {
  const { asOf } = useSession();
  const [choice, setChoice] = useState(envs[0] ?? "");
  const env = envs.includes(choice) ? choice : envs[0];
  const path = asOf === null && env !== undefined ? observationPath(env) : null;
  const observation = useApi<Observation>(path, { live: anyEvent, pollMs: POLL_MS });
  if (path === null || observation.status === NOT_FOUND) {
    return null;
  }
  const aside = envs.length > 1 ? (
    <Dropdown id="components-env" titleText="Environment" hideLabel label="Environment"
      size="sm" items={envs} selectedItem={env}
      onChange={({ selectedItem }) => setChoice(selectedItem ?? "")} />
  ) : <Tag type="outline" size="sm">{env}</Tag>;
  return (
    <Section title="Components" aside={aside}>
      <Loaded state={observation} what={`the components of ${env}`}>
        {(data) => <ComponentsView observation={data} />}
      </Loaded>
    </Section>
  );
}
