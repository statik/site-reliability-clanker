import { useState } from "react";
import {
  Accordion,
  AccordionItem,
  CodeSnippet,
  StructuredListBody,
  StructuredListCell,
  StructuredListRow,
  StructuredListWrapper,
} from "@carbon/react";
import type { Finding, PlanJSON, WorkItemDetail } from "@/api/types";
import { AppLink, Loaded } from "@/components/common";
import { DiffView } from "@/components/DiffView";
import { StatusTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <StructuredListRow>
      <StructuredListCell noWrap>{label}</StructuredListCell>
      <StructuredListCell>{children}</StructuredListCell>
    </StructuredListRow>
  );
}

function RawPlan({ workItem, layer }: { workItem: string; layer: string }) {
  const plan = useApi<PlanJSON>(`/work-items/${workItem}/plans/${layer}`);
  return (
    <Loaded state={plan} what={`the raw plan of ${layer}`}>
      {(data) => (
        <CodeSnippet type="multi" feedback="Copied" className="app-full"
          aria-label={`Raw plan JSON of ${layer}`} maxCollapsedNumberOfRows={24}>
          {JSON.stringify(data.plan, null, 2)}
        </CodeSnippet>
      )}
    </Loaded>
  );
}

function PlanItem({ workItem, layer, hash }: { workItem: string; layer: string; hash: string }) {
  const [opened, setOpened] = useState(false);
  return (
    <AccordionItem title={`Raw plan JSON of layer ${layer}`}
      onHeadingClick={({ isOpen }) => setOpened((was) => was || isOpen)}>
      <p className="app-label">Plan SHA-256</p>
      <p className="app-mono">{hash}</p>
      {opened && <RawPlan workItem={workItem} layer={layer} />}
    </AccordionItem>
  );
}

function FindingRow({ workItem }: { workItem: string }) {
  const findings = useApi<Finding[]>("/findings");
  const finding = (findings.data ?? []).find((candidate) => candidate.work_item === workItem);
  if (finding === undefined) {
    return null;
  }
  return (
    <Row label="Finding">
      {finding.title} (detector {finding.detector}, {finding.state})
    </Row>
  );
}

/** Section 7 of the approval page: the trigger, the pull request, the plans and the hashes. */
export function Evidence({ detail }: { detail: WorkItemDetail }) {
  const item = detail.work_item;
  const plans = (detail.plans ?? []).filter((plan) => !plan.superseded);
  const digests = [...new Set((detail.jobs ?? []).map((job) => job.tool_digest))]
    .filter((digest) => digest !== "");
  return (
    <div className="app-stack-tight">
      <StructuredListWrapper isCondensed aria-label="Evidence">
        <StructuredListBody>
          <Row label="Trigger">{item.trigger === "" ? "Not recorded" : item.trigger}</Row>
          <FindingRow workItem={item.id} />
          {item.rollback_of !== "" && (
            <Row label="Rolls back">
              <AppLink to={`/work/${item.rollback_of}`}>{item.rollback_of}</AppLink>
            </Row>
          )}
          <Row label="Pull request">
            {detail.pull === undefined ? "None. This work item changes no code." : (
              <span className="app-row">
                <AppLink to={`/code/pulls/${detail.pull.id}`}>
                  #{detail.pull.id} {detail.pull.title}
                </AppLink>
                <StatusTag status={detail.pull.state} size="sm" />
              </span>
            )}
          </Row>
          <Row label="Commit">
            <span className="app-mono">{item.commit === "" ? "None yet" : item.commit}</span>
          </Row>
          <Row label="Policy result">
            {plans.length === 0 ? `Tier ${item.tier || "not set"}, rule ${item.tier_rule || "none"}`
              : plans.map((plan) => (
                <div key={plan.id}>
                  {plan.layer}: tier {plan.policy.tier} by rule {plan.policy.rule}
                </div>
              ))}
          </Row>
          <Row label="Tool digest">
            {digests.length === 0 ? "No job has run yet"
              : digests.map((digest) => <div key={digest} className="app-mono">{digest}</div>)}
          </Row>
        </StructuredListBody>
      </StructuredListWrapper>
      <Accordion align="start">
        {detail.diff !== undefined && (
          <AccordionItem title="Pull request diff">
            <DiffView diff={detail.diff} />
          </AccordionItem>
        )}
        {plans.map((plan) => (
          <PlanItem key={plan.id} workItem={item.id} layer={plan.layer} hash={plan.plan_sha256} />
        ))}
      </Accordion>
    </div>
  );
}
