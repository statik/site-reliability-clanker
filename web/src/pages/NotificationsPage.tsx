import { Link, Tag, Tile } from "@carbon/react";
import { Launch } from "@carbon/icons-react";
import type { Delivery, NotificationRoute, Notifications } from "@/api/types";
import { AppLink, Loaded, PageHeader, Section } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StatusTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { formatTime } from "@/lib/time";

const MAIL_INBOX = "http://127.0.0.1:8025";
const WEBHOOK_SINK = "http://127.0.0.1:7071";

const ROUTE_COLUMNS: Column<NotificationRoute>[] = [
  { key: "name", header: "Rule", value: (row) => row.name },
  { key: "types", header: "Event types", value: (row) => (row.types ?? []).join(", ") },
  { key: "scope", header: "Tiers and environments",
    value: (row) => [...(row.tiers ?? []), ...(row.envs ?? [])].join(", ") || "All" },
  { key: "audience", header: "Who", value: (row) => row.audience },
  { key: "channels", header: "Channels", value: (row) => row.channels.join(", ") },
  { key: "urgency", header: "Urgency", value: (row) => row.urgency,
    render: (row) => (
      <Tag type={row.urgency === "immediate" ? "red" : "gray"} size="sm">
        {row.urgency === "immediate" ? "Immediate" : "Daily digest"}
      </Tag>
    ) },
];

const DELIVERY_COLUMNS: Column<Delivery>[] = [
  { key: "at", header: "Time", value: (row) => row.at, render: (row) => formatTime(row.at) },
  { key: "channel", header: "Channel", value: (row) => row.channel },
  { key: "target", header: "Delivered to", value: (row) => row.target },
  { key: "subject", header: "Subject", value: (row) => row.subject },
  { key: "status", header: "Status", value: (row) => row.status,
    render: (row) => <StatusTag status={row.status} size="sm" /> },
  { key: "attempts", header: "Attempts", value: (row) => row.attempts },
  { key: "error", header: "Error", value: (row) => row.error || "–" },
  { key: "event", header: "Event", value: (row) => row.event_seq,
    render: (row) => <AppLink to={`/audit?seq=${row.event_seq}`}>{row.event_seq}</AppLink> },
];

function Body({ data }: { data: Notifications }) {
  const [repo, ...file] = data.rules_source.split(":");
  return (
    <>
      <Section title="Channels">
        <div className="app-grid">
          {data.channels.map((channel) => (
            <Tile key={channel.channel} className="app-stack-tight">
              <div className="app-row app-spread">
                <h3 className="app-heading">{channel.channel}</h3>
                <StatusTag status={channel.configured ? "ok" : "not deployed"} size="sm"
                  label={channel.configured ? "Configured" : "Not configured"} />
              </div>
              <p className="app-label">Delivers to</p>
              {(channel.targets ?? []).map((target) => <p key={target}>{target}</p>)}
            </Tile>
          ))}
        </div>
        <div className="app-row">
          <Link href={MAIL_INBOX} target="_blank" rel="noreferrer" renderIcon={Launch}>
            Local mail inbox ({MAIL_INBOX})
          </Link>
          <Link href={WEBHOOK_SINK} target="_blank" rel="noreferrer" renderIcon={Launch}>
            Webhook sink for Slack, Teams and Discord ({WEBHOOK_SINK})
          </Link>
        </div>
      </Section>
      <Section title="Routing rules">
        <SimpleTable title="Who is told about what"
          description={`The rules are code: ${data.rules_source}. A change to them is a pull ` +
            "request like any other change."}
          rows={data.rules.routes} rowId={(row) => row.name} columns={ROUTE_COLUMNS}
          empty="No routing rule is defined." toolbar={
            <AppLink to={`/code/${repo ?? ""}?path=${file.join(":")}&file=1`}>
              Open the rules file
            </AppLink>
          } />
      </Section>
      <Section title="Delivery history">
        <SimpleTable title="Deliveries" description="The newest 200 deliveries."
          rows={data.deliveries} rowId={(row) => String(row.id)} columns={DELIVERY_COLUMNS}
          size="sm" empty="Nothing has been delivered yet." />
      </Section>
    </>
  );
}

/** Who gets told about what, on which channel, and the delivery history. */
export function NotificationsPage() {
  const notifications = useApi<Notifications>("/notifications", {
    live: (event) => event.type.startsWith("notify.") || event.type.startsWith("approval.") ||
      event.type.startsWith("incident.") || event.type.startsWith("apply."),
  });
  return (
    <>
      <PageHeader title="Notifications">
        Events are routed to email, Slack, Microsoft Teams and Discord. In this prototype the
        channels deliver to a local mail inbox and a local webhook sink.
      </PageHeader>
      <Loaded state={notifications} what="the notifications">
        {(data) => <Body data={data} />}
      </Loaded>
    </>
  );
}
