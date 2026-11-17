import { useSearchParams } from "react-router-dom";
import {
  InlineNotification,
  StructuredListBody,
  StructuredListCell,
  StructuredListHead,
  StructuredListRow,
  StructuredListWrapper,
} from "@carbon/react";
import { Locked } from "@carbon/icons-react";
import type { Access, Grant } from "@/api/types";
import { Empty, Loaded, PageHeader,
  ScrollRegion, Section, WorkItemLink } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StatusTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { formatRelative, shortHash } from "@/lib/time";
import { GrantLanes } from "@/pages/access/GrantLanes";
import { GRANT_COLUMNS } from "@/pages/workitem/Records";
import { useServerNow } from "@/state/clock";
import { useSession } from "@/state/session";

const WORK_ITEM_COLUMN: Column<Grant> = {
  key: "work_item", header: "Work item", value: (row) => row.work_item,
  render: (row) => <WorkItemLink id={row.work_item} />,
};
const HISTORY_COLUMNS = [...GRANT_COLUMNS.slice(0, 1), WORK_ITEM_COLUMN, ...GRANT_COLUMNS.slice(1)];

function GrantDetail({ grant }: { grant: Grant }) {
  return (
    <div className="app-stack-tight">
      <p className="app-label">Bound to commit and plan</p>
      <p className="app-mono">commit {grant.commit || "none"}</p>
      <p className="app-mono">plan sha256 {grant.plan_sha256 || "none"}</p>
      <p className="app-label">
        Valid from {grant.not_before} to {grant.expires} · tier {grant.tier}
      </p>
      <p className="app-mono">signature {shortHash(grant.signature, 32)}…</p>
    </div>
  );
}

function ActiveGrants({ grants, now }: { grants: Grant[]; now: Date }) {
  if (grants.length === 0) {
    return (
      <Empty icon={<Locked size={32} />}>
        No grant is active. Nothing holds write access right now.
      </Empty>
    );
  }
  return (
    <div className="app-grid">
      {grants.map((grant) => (
        <InlineNotification key={grant.id} kind="warning" lowContrast hideCloseButton
          title={`${grant.identity} holds write access`}
          subtitle={`${grant.id}: ${grant.kind} ${grant.layer} in ${grant.env} for ` +
            `${grant.work_item}. Expires ${formatRelative(grant.expires, now)}.`} />
      ))}
    </div>
  );
}

function Body({ access }: { access: Access }) {
  const now = useServerNow();
  const [search] = useSearchParams();
  const cited = search.get("grant");
  const history = cited === null ? access.history
    : access.history.filter((grant) => grant.id === cited);
  return (
    <>
      <Section title="Active grants now">
        <ActiveGrants grants={access.active} now={now} />
      </Section>
      <Section title="Access over time">
        <GrantLanes grants={access.history} now={now}
          identities={access.identities.map((identity) => identity.name)} />
      </Section>
      <Section title="Identities and standing access">
        <ScrollRegion label="Identities">
          <StructuredListWrapper isCondensed aria-label="Identities">
            <StructuredListHead>
              <StructuredListRow head>
                <StructuredListCell head>Identity</StructuredListCell>
                <StructuredListCell head>Standing access</StructuredListCell>
                <StructuredListCell head>Elevation</StructuredListCell>
                <StructuredListCell head>Now</StructuredListCell>
              </StructuredListRow>
            </StructuredListHead>
            <StructuredListBody>
              {access.identities.map((identity) => {
                const held = access.active.some((grant) => grant.identity === identity.name);
                return (
                  <StructuredListRow key={identity.name}>
                    <StructuredListCell noWrap>
                      <span className="app-mono">{identity.name}</span>
                    </StructuredListCell>
                    <StructuredListCell>{identity.standing}</StructuredListCell>
                    <StructuredListCell>{identity.elevation}</StructuredListCell>
                    <StructuredListCell noWrap>
                      <StatusTag status={held ? "active" : "used"} size="sm"
                        label={held ? "Grant active" : "No grant"} />
                    </StructuredListCell>
                  </StructuredListRow>
                );
              })}
            </StructuredListBody>
          </StructuredListWrapper>
        </ScrollRegion>
      </Section>
      <Section title="Grant history">
        <SimpleTable title="Grants"
          description={cited === null ? `${history.length} grants, newest first`
            : `Showing the cited grant ${cited}`}
          rows={history} rowId={(row) => row.id} columns={HISTORY_COLUMNS} size="sm"
          expand={(row) => <GrantDetail grant={row} />} empty="No grant has been issued." />
      </Section>
    </>
  );
}

/** Who and what holds which permission right now, and the history of grants. */
export function AccessPage() {
  const { asOf } = useSession();
  const access = useApi<Access>("/access", { live: (event) => event.type.startsWith("grant.") ||
    event.type.startsWith("apply.") || event.type.startsWith("runbook.") });
  return (
    <>
      <PageHeader title="Access">
        The agent has no standing write access. A grant gives one identity access to one
        layer for one approved plan, and it is revoked when the job ends.
      </PageHeader>
      {asOf !== null && (
        <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
          title="This page always shows the live state."
          subtitle="The server does not offer grants as of a past time. Use the lane chart." />
      )}
      <Loaded state={access} what="the access record">
        {(data) => <Body access={data} />}
      </Loaded>
    </>
  );
}
