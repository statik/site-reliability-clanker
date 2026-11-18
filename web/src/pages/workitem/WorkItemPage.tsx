import { useParams } from "react-router-dom";
import {
  Breadcrumb,
  BreadcrumbItem,
  ListItem,
  StructuredListBody,
  StructuredListCell,
  StructuredListHead,
  StructuredListRow,
  StructuredListWrapper,
  Tag,
  Tile,
  UnorderedList,
} from "@carbon/react";
import { Link as RouterLink } from "react-router-dom";
import { waitNote } from "@/api/decision";
import type { AuditEvent, Plan, WorkItem, WorkItemDetail } from "@/api/types";
import { AskPanel } from "@/components/AskPanel";
import { AiMark, Loaded, ScrollRegion, Section } from "@/components/common";
import { StateTag, TierTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { permissionRequests } from "@/lib/permissions";
import { formatTime } from "@/lib/time";
import { Changes } from "@/pages/workitem/Changes";
import { Decide } from "@/pages/workitem/Decide";
import { Evidence } from "@/pages/workitem/Evidence";
import { Records } from "@/pages/workitem/Records";
import { StateProgress } from "@/pages/workitem/StateProgress";

const SCOPED_QUESTIONS = [
  "What happens if I leave this a week?",
  "What exactly will change?",
  "How is this rolled back?",
];

function Fact({ label, children }: { label: string; children: string }) {
  return (
    <Tile>
      <p className="app-label">{label}</p>
      <p>{children === "" ? "Not stated" : children}</p>
    </Tile>
  );
}

function Impact({ item }: { item: WorkItem }) {
  const text = item.explanation;
  return (
    <div className="app-grid-4">
      <Fact label="Expected downtime">{text.downtime}</Fact>
      <Fact label="Effect on users">{text.user_effect}</Fact>
      <Fact label="Cost change">{text.cost_change}</Fact>
      <Fact label="How long it takes">{text.duration}</Fact>
    </div>
  );
}

function TierRule({ item, plans }: { item: WorkItem; plans: Plan[] }) {
  const reasons = [...new Set(plans.filter((plan) => !plan.superseded)
    .flatMap((plan) => plan.policy.reasons ?? []))];
  return (
    <Tile className="app-stack-tight">
      <div className="app-row">
        <TierTag tier={item.tier} />
        <span>
          Set by policy rule{" "}
          <span className="app-mono">{item.tier_rule === "" ? "none yet" : item.tier_rule}</span>
          , from the plan. The model does not choose the tier.
        </span>
      </div>
      {reasons.length > 0 && (
        <UnorderedList>
          {reasons.map((reason) => <ListItem key={reason}>{reason}</ListItem>)}
        </UnorderedList>
      )}
    </Tile>
  );
}

function Safety({ item }: { item: WorkItem }) {
  const text = item.explanation;
  const checks = text.pre_checks ?? [];
  return (
    <Tile className="app-stack-tight">
      <p className="app-label">Pre-checks</p>
      {checks.length === 0 ? <p>None stated.</p> : (
        <UnorderedList>
          {checks.map((check) => <ListItem key={check}>{check}</ListItem>)}
        </UnorderedList>
      )}
      <p className="app-label">Backup first</p>
      <p>{text.backup === "" ? "Not stated" : text.backup}</p>
      <p className="app-label">Rollback</p>
      <p>{text.rollback === "" ? "Not stated" : text.rollback}</p>
      <p className="app-label">Verification after</p>
      <p>{text.verification === "" ? "Not stated" : text.verification}</p>
    </Tile>
  );
}

function Permissions({ item }: { item: WorkItem }) {
  const requests = permissionRequests(item);
  if (requests.length === 0) {
    return <Tile>This work item asks for no write permission.</Tile>;
  }
  return (
    <ScrollRegion label="Permissions requested">
      <StructuredListWrapper isCondensed aria-label="Permissions requested">
        <StructuredListHead>
          <StructuredListRow head>
            <StructuredListCell head>Identity</StructuredListCell>
            <StructuredListCell head>What it may do</StructuredListCell>
            <StructuredListCell head>For how long</StructuredListCell>
          </StructuredListRow>
        </StructuredListHead>
        <StructuredListBody>
          {requests.map((request) => (
            <StructuredListRow key={request.identity}>
              <StructuredListCell noWrap>
                <span className="app-mono">{request.identity}</span>
              </StructuredListCell>
              <StructuredListCell>{request.scope}</StructuredListCell>
              <StructuredListCell>{request.duration}</StructuredListCell>
            </StructuredListRow>
          ))}
        </StructuredListBody>
      </StructuredListWrapper>
    </ScrollRegion>
  );
}

function Heading({ item }: { item: WorkItem }) {
  return (
    <header className="app-page-header app-stack-tight">
      <Breadcrumb noTrailingSlash>
        <BreadcrumbItem><RouterLink to="/activity">Activity</RouterLink></BreadcrumbItem>
        <BreadcrumbItem isCurrentPage>{item.id}</BreadcrumbItem>
      </Breadcrumb>
      <h1>{item.title}</h1>
      <div className="app-row">
        <StateTag state={item.state} />
        <TierTag tier={item.tier} />
        <Tag type="outline">{item.env}</Tag>
        <Tag type="outline">{item.kind}</Tag>
        <span className="app-label">
          Opened {formatTime(item.created_at)} · updated {formatTime(item.updated_at)}
          {waitNote(item) !== "" && ` · ${waitNote(item)}`}
        </span>
      </div>
    </header>
  );
}

/** The investigator's diagnosis of an incident, from its audit event. */
function Diagnosis({ events }: { events: AuditEvent[] }) {
  const event = events.findLast((candidate) => candidate.type === "incident.diagnosed");
  if (event === undefined) {
    return null;
  }
  const field = (key: string) => {
    const value = event.data?.[key];
    return typeof value === "string" ? value : "";
  };
  return (
    <Tile className="app-stack-tight" decorator={
      <AiMark source={field("model")} what="diagnosis" />
    }>
      <p className="app-label">Investigator, {formatTime(event.ts)}</p>
      <p className="app-prose">{event.summary}</p>
      {field("evidence") !== "" && <p className="app-prose">Evidence: {field("evidence")}</p>}
      {field("proposed_runbook") !== "" && (
        <p className="app-label">
          Proposed mitigation: runbook {field("proposed_runbook")} on{" "}
          {field("deployment") || "no deployment"}
        </p>
      )}
    </Tile>
  );
}

/**
 * The adversarial review of the plan, from its audit event. The reviewer is a second model
 * that looks for reasons for a higher tier. It can raise the tier and never lower it.
 */
function AdversarialReview({ events }: { events: AuditEvent[] }) {
  const event = events.findLast((candidate) => candidate.type === "review.completed");
  if (event === undefined) {
    return null;
  }
  const field = (key: string) => {
    const value = event.data?.[key];
    return typeof value === "string" ? value : "";
  };
  const listed = event.data?.["concerns"];
  const concerns = Array.isArray(listed)
    ? listed.filter((concern): concern is string => typeof concern === "string")
    : [];
  return (
    <Tile className="app-stack-tight" decorator={<AiMark source={field("model")} what="review" />}>
      <p className="app-label">Adversarial review, {formatTime(event.ts)}</p>
      <p className="app-prose">
        {event.summary}. The policy engine set tier {field("policy_tier")} by rule{" "}
        <span className="app-mono">{field("policy_rule")}</span>. The review can raise the tier.
        It cannot lower it.
      </p>
      {concerns.length > 0 && (
        <ul className="app-prose">
          {concerns.map((concern) => <li key={concern}>{concern}</li>)}
        </ul>
      )}
    </Tile>
  );
}

function Detail({ detail, reload }: { detail: WorkItemDetail; reload: () => void }) {
  const item = detail.work_item;
  const text = item.explanation;
  const mark = <AiMark source={text.source} what="explanation" />;
  const incident = item.kind === "incident";
  return (
    <div className="app-narrow">
      <Heading item={item} />
      <StateProgress workItem={item} events={detail.events ?? []} />
      <Section title={incident ? "1. Diagnosis" : "1. In one sentence"}>
        <Tile className="app-stack-tight" decorator={mark}>
          <p className="app-big">
            {text.sentence === "" ? item.summary || item.title : text.sentence}
          </p>
          {text.why !== "" && <p className="app-prose">{text.why}</p>}
        </Tile>
        <Diagnosis events={detail.events ?? []} />
      </Section>
      <Section title="2. Impact" aside={mark}><Impact item={item} /></Section>
      <Section title="3. Risk tier and the rule that set it">
        <TierRule item={item} plans={detail.plans ?? []} />
        <AdversarialReview events={detail.events ?? []} />
      </Section>
      <Section title="4. What changes">
        <Changes plans={detail.plans ?? []} deferred={detail.deferred_layers ?? []} />
      </Section>
      <Section title="5. Safety" aside={mark}><Safety item={item} /></Section>
      <Section title="6. Permissions requested"><Permissions item={item} /></Section>
      <Section title="7. Evidence"><Evidence detail={detail} /></Section>
      <Section title="8. Ask about this change">
        <AskPanel workItem={item.id} suggestions={SCOPED_QUESTIONS} />
      </Section>
      <Section title="9. Decide"><Decide detail={detail} onDecided={reload} /></Section>
      <Section title="Record"><Records detail={detail} /></Section>
    </div>
  );
}

/** The approval page of PLAN 7.2, which is also the detail page of every work item. */
export function WorkItemPage() {
  const { id = "" } = useParams();
  const detail = useApi<WorkItemDetail>(`/work-items/${encodeURIComponent(id)}`, {
    live: (event) => event.work_item === id,
  });
  return (
    <Loaded state={detail} what={`work item ${id}`}>
      {(data) => <Detail detail={data} reload={detail.reload} />}
    </Loaded>
  );
}
