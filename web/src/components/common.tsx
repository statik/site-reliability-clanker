import type { ReactNode } from "react";
import { Link as RouterLink } from "react-router-dom";
import { AILabel, AILabelContent, InlineLoading, InlineNotification, Link, Tile }
  from "@carbon/react";
import type { ApiState } from "@/hooks/useApi";

/** Shows an error from the server with Carbon's inline notification. */
export function ErrorNote({ title, message }: { title: string; message: string }) {
  return (
    <InlineNotification kind="error" lowContrast hideCloseButton title={title}
      subtitle={message} className="app-note" />
  );
}

/**
 * Renders loaded data. While a reload fails it keeps the last good data on screen and shows
 * the error above it.
 */
export function Loaded<T>({ state, what, children }: {
  state: ApiState<T>;
  what: string;
  children: (data: T) => ReactNode;
}) {
  return (
    <>
      {state.error !== null && (
        <ErrorNote title={`Could not load ${what}.`}
          message={`${state.error} The view tries again on the next event.`} />
      )}
      {state.data === null && state.loading && <InlineLoading description={`Loading ${what}`} />}
      {state.data !== null && children(state.data)}
    </>
  );
}

export function PageHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <header className="app-page-header">
      <h1>{title}</h1>
      {children !== undefined && <p>{children}</p>}
    </header>
  );
}

export function Section({ title, children, aside }: {
  title: string;
  children: ReactNode;
  aside?: ReactNode;
}) {
  return (
    <section className="app-section">
      <div className="app-section-title">
        <h2>{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** An empty state: an icon, a sentence that says why it is empty, and nothing else. */
export function Empty({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <Tile className="app-empty">
      {icon}
      <p>{children}</p>
    </Tile>
  );
}

/**
 * Holds content that cannot get narrower than a phone, such as a table. The content scrolls
 * sideways inside this region and the page does not. The region takes focus, so the arrow
 * keys scroll it.
 */
export function ScrollRegion({ label, className, children }: {
  label: string;
  className?: string;
  children: ReactNode;
}) {
  const classes = className === undefined ? "app-scroll-x" : `app-scroll-x ${className}`;
  return (
    <div className={classes} role="region" aria-label={`${label}, scrolls sideways`}
      // A region that scrolls must take focus, or a keyboard cannot scroll it (WCAG 2.1.1).
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}>
      {children}
    </div>
  );
}

export function WorkItemLink({ id, children }: { id: string; children?: ReactNode }) {
  if (id === "") {
    return <>–</>;
  }
  return <Link as={RouterLink} to={`/work/${id}`}>{children ?? id}</Link>;
}

export function AppLink({ to, children }: { to: string; children: ReactNode }) {
  return <Link as={RouterLink} to={to}>{children}</Link>;
}

/**
 * Marks text that a model, a recording or a template wrote. The popover names the writer and
 * says that the facts beside the text come from the plan.
 */
export function AiMark({ source, what }: { source: string; what: string }) {
  const writer = source === "" ? "an unrecorded source" : source;
  return (
    <AILabel autoAlign size="xs" aria-label={`AI: ${what} written by ${writer}`}>
      <AILabelContent>
        <div className="app-ai-content">
          <p className="app-label">AI explained</p>
          <p className="app-ai-writer">{describeSource(source)}</p>
          <p>
            This {what} is generated text. The facts beside it (counts, resources, tier,
            hashes, times) come from the plan and the audit log, not from the model.
          </p>
        </div>
      </AILabelContent>
    </AILabel>
  );
}

function describeSource(source: string): string {
  if (source === "template") {
    return "Written by a template, with no model";
  }
  if (source === "replay") {
    return "Replay of a recorded model answer";
  }
  if (source === "") {
    return "The server did not record what wrote this";
  }
  return `Written by model ${source}`;
}
