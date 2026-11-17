import { useState } from "react";
import {
  Button,
  InlineNotification,
  NumberInput,
  StructuredListBody,
  StructuredListCell,
  StructuredListHead,
  StructuredListRow,
  StructuredListWrapper,
  Tile,
} from "@carbon/react";
import { SkipForward, Play } from "@carbon/icons-react";
import { apiPost, errorMessage } from "@/api/client";
import type { SimState } from "@/api/types";
import { AskPanel } from "@/components/AskPanel";
import { ErrorNote, Loaded, PageHeader, ScrollRegion, Section } from "@/components/common";
import { StatusTag } from "@/components/status";
import { anyEvent, useApi } from "@/hooks/useApi";
import { formatDuration, formatTime } from "@/lib/time";

/** The questions of PLAN 7.3. */
const QUESTIONS = [
  "What is waiting on me and what happens if I leave it a week?",
  "Are we behind on any patches? Which ones matter?",
  "What changed in production in the last 30 days, and who approved each change?",
  "When was the last successful restore test?",
  "Why did last night's upgrade take longer than planned?",
  "What will next month's maintenance look like?",
  "What does the agent have access to right now?",
];

/** Chat grounded in the work items, the audit log and the grants. */
export function AskPage() {
  return (
    <div className="app-narrow">
      <PageHeader title="Ask">
        Ask about the state of the platform. Each answer lists its sources, so every claim is
        one click from the work item, event or grant behind it.
      </PageHeader>
      <AskPanel workItem="" suggestions={QUESTIONS} />
    </div>
  );
}

function useAction(onDone: () => void) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const run = (name: string, path: string, body: unknown, done: string) => {
    setBusy(name);
    setError(null);
    setNote(null);
    apiPost<unknown>(path, body).then(() => setNote(done),
      (failure: unknown) => setError(errorMessage(failure)))
      .finally(() => {
        setBusy(null);
        onDone();
      });
  };
  return { busy, error, note, run };
}

function Simulator({ sim, reload }: { sim: SimState; reload: () => void }) {
  const [hours, setHours] = useState(24);
  const action = useAction(reload);
  return (
    <div className="app-stack">
      <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
        title="A scene only injects inputs."
        subtitle={"It publishes a release or an advisory, changes something by hand, or " +
          "injects a fault. The control plane then reacts through its normal pipeline."} />
      {action.error !== null && <ErrorNote title="The simulator refused." message={action.error} />}
      {action.note !== null && (
        <InlineNotification kind="success" lowContrast hideCloseButton className="app-note"
          title={action.note} />
      )}
      <Section title="Virtual clock">
        <Tile className="app-row app-row-end">
          <div className="app-fill">
            <p className="app-label">Server time</p>
            <p className="app-big">{formatTime(sim.now)}</p>
            <p className="app-label">
              {sim.offset_seconds === 0 ? "The clock has not been advanced."
                : `${formatDuration(sim.offset_seconds * 1000)} ahead of real time.`}
            </p>
          </div>
          <NumberInput id="sim-hours" label="Hours to advance" min={1} max={1440} value={hours}
            onChange={(_, state) => setHours(Number(state.value))} />
          <Button renderIcon={SkipForward} disabled={action.busy !== null}
            onClick={() => action.run("advance", "/sim/advance", { hours },
              `The clock advanced by ${hours} hours.`)}>
            Advance the clock
          </Button>
        </Tile>
      </Section>
      <Section title="Scenes">
        <ScrollRegion label="Scenes">
          <StructuredListWrapper aria-label="Scenes">
            <StructuredListHead>
              <StructuredListRow head>
                <StructuredListCell head>Scene</StructuredListCell>
                <StructuredListCell head>What it injects</StructuredListCell>
                <StructuredListCell head>Use case</StructuredListCell>
                <StructuredListCell head>Status</StructuredListCell>
                <StructuredListCell head>Run</StructuredListCell>
              </StructuredListRow>
            </StructuredListHead>
            <StructuredListBody>
              {sim.scenes.map((scene) => (
                <StructuredListRow key={scene.name}>
                  <StructuredListCell>{scene.title}</StructuredListCell>
                  <StructuredListCell>{scene.detail}</StructuredListCell>
                  <StructuredListCell noWrap>{scene.use_case}</StructuredListCell>
                  <StructuredListCell noWrap>
                    <StatusTag size="sm" status={scene.done ? "succeeded" : "open"}
                      label={scene.done ? "Done" : scene.name === sim.next ? "Next" : "Not run"} />
                  </StructuredListCell>
                  <StructuredListCell>
                    <Button size="sm" kind={scene.name === sim.next ? "primary" : "tertiary"}
                      renderIcon={Play} disabled={action.busy !== null}
                      onClick={() => action.run(scene.name, `/sim/scenes/${scene.name}`, {},
                        `Scene "${scene.title}" started.`)}>
                      Run
                    </Button>
                  </StructuredListCell>
                </StructuredListRow>
              ))}
            </StructuredListBody>
          </StructuredListWrapper>
        </ScrollRegion>
      </Section>
    </div>
  );
}

/** The simulator: the virtual clock and the scripted scenes of the demo. */
export function DemoPage() {
  const sim = useApi<SimState>("/sim", { live: anyEvent });
  return (
    <div className="app-narrow">
      <PageHeader title="Demo">
        The simulator drives the demo month: it moves the clock and plays outside events.
      </PageHeader>
      <Loaded state={sim} what="the simulator">
        {(data) => <Simulator sim={data} reload={sim.reload} />}
      </Loaded>
    </div>
  );
}
