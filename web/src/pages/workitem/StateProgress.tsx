import { ProgressIndicator, ProgressStep } from "@carbon/react";
import type { AuditEvent, WorkItem } from "@/api/types";
import { stateLabel } from "@/components/status";
import { stateSteps, TERMINAL_STATES } from "@/lib/stateDurations";
import type { StateStep } from "@/lib/stateDurations";
import { formatDuration } from "@/lib/time";
import { useServerNow } from "@/state/clock";
import { ScrollRegion } from "@/components/common";

const BAD_STATES = ["Failed", "RolledBack", "Rejected"];

function secondary(step: StateStep): string {
  if (step.status === "skipped") {
    return "Skipped";
  }
  if (step.status === "upcoming") {
    return "Not reached";
  }
  if (TERMINAL_STATES.includes(step.state) && step.status === "current") {
    return "Final state";
  }
  const visits = step.visits > 1 ? ` over ${step.visits} visits` : "";
  return `${formatDuration(step.ms)}${visits}`;
}

/** The state machine of one work item, with the time spent in each state. */
export function StateProgress({ workItem, events }: {
  workItem: WorkItem;
  events: AuditEvent[];
}) {
  const now = useServerNow();
  const steps = stateSteps(workItem.created_at, events, now);
  const currentIndex = steps.findIndex((step) => step.status === "current");
  return (
    <ScrollRegion label="Progress through the states" className="app-progress">
      <ProgressIndicator currentIndex={currentIndex} spaceEqually
        aria-label="Progress through the work item states">
        {steps.map((step) => (
          <ProgressStep key={step.state} label={stateLabel(step.state)}
            secondaryLabel={secondary(step)}
            description={`${stateLabel(step.state)}: ${secondary(step)}`}
            complete={step.status === "complete"} current={step.status === "current"}
            disabled={step.status === "skipped"}
            invalid={BAD_STATES.includes(step.state) && step.status !== "upcoming"} />
        ))}
      </ProgressIndicator>
    </ScrollRegion>
  );
}
