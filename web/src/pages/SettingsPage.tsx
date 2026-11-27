import { useState } from "react";
import { Button, InlineNotification, Tag, Tile } from "@carbon/react";
import { PlayFilledAlt, StopFilledAlt } from "@carbon/icons-react";
import { apiPost, errorMessage } from "@/api/client";
import type { LocalInference, ModelSettings, User } from "@/api/types";
import { ErrorNote, Loaded, PageHeader, Section } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StatusTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { useSession } from "@/state/session";

const MODE_MEANING: Record<string, string> = {
  openrouter: "Each role calls its model through OpenRouter, with the key of this control plane.",
  local: "Each role calls a model on an endpoint that the operator runs, and no prompt goes " +
    "to a hosted provider.",
  replay: "Answers are recorded, and no model is called.",
  bedrock: "Each role calls an inference profile of Amazon Bedrock in the AWS account of " +
    "this control plane. Bedrock bills each token.",
};

interface RoleModel {
  role: string;
  model: string;
}

const MODEL_COLUMNS: Column<RoleModel>[] = [
  { key: "role", header: "Role", value: (row) => row.role },
  { key: "model", header: "Model", value: (row) => row.model },
];

/**
 * Says whether this user can start and stop the local inference, and when not, why. The
 * server checks the same rule.
 */
export function localControl(user: User | null, local: LocalInference):
{ allowed: boolean; reason: string } {
  if (!local.available) {
    return { allowed: false, reason: local.reason ?? "This control plane cannot control it." };
  }
  if (user?.role !== "admin" && user?.role !== "approver") {
    return { allowed: false,
      reason: `Only an admin or an approver starts or stops the local inference. You are ` +
        `${user?.display ?? "a user"} (${user?.role ?? "unknown role"}).` };
  }
  return { allowed: true, reason: "" };
}

function modelState(local: LocalInference): string {
  if (!local.running) {
    return `The server does not answer at ${local.url ?? "its address"}.`;
  }
  if (local.model_loaded) {
    return `The model ${local.model ?? ""} is in memory.`;
  }
  if (local.model_present) {
    return `The server holds the model ${local.model ?? ""}. It is not in memory.`;
  }
  return `The server does not hold the model ${local.model ?? ""}.`;
}

function LocalTile({ local, user, reload }: {
  local: LocalInference;
  user: User | null;
  reload: () => void;
}) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const control = localControl(user, local);
  const run = (action: "start" | "stop") => {
    setBusy(action);
    setError(null);
    setDone(null);
    apiPost<ModelSettings>(`/settings/model/local/${action}`, {}).then(
      (answer) => setDone(answer.done ?? ""),
      (failure: unknown) => setError(errorMessage(failure)),
    ).finally(() => {
      setBusy(null);
      reload();
    });
  };
  return (
    <Tile className="app-stack-tight">
      <div className="app-row app-spread">
        <h3 className="app-heading">Local inference</h3>
        {local.available && (
          <StatusTag status={local.running ? "running" : "not deployed"} size="sm"
            label={local.running ? `Ollama ${local.version ?? ""} runs` : "Not running"} />
        )}
      </div>
      {local.available && (
        <p>
          {modelState(local)}
          {local.started_by_src && " S.R.C. started the server, and Stop stops it."}
          {local.running && !local.started_by_src &&
            " S.R.C. did not start the server, so Stop unloads the model and leaves the server."}
        </p>
      )}
      {!control.allowed && <p className="app-secondary">{control.reason}</p>}
      {error !== null && (
        <ErrorNote title="The local inference did not change." message={error} />
      )}
      {done !== null && (
        <InlineNotification kind="success" lowContrast hideCloseButton className="app-note"
          title="Done." subtitle={done} />
      )}
      <div className="app-row">
        <Button size="sm" renderIcon={PlayFilledAlt} disabled={!control.allowed || busy !== null}
          onClick={() => run("start")}>
          {busy === "start" ? "Starting" : "Start"}
        </Button>
        <Button size="sm" kind="tertiary" renderIcon={StopFilledAlt}
          disabled={!control.allowed || busy !== null} onClick={() => run("stop")}>
          {busy === "stop" ? "Stopping" : "Stop"}
        </Button>
      </div>
    </Tile>
  );
}

function keyText(data: ModelSettings): string {
  if (data.mode === "bedrock") {
    return "No key. The AWS credentials of the control plane sign each call: on AWS, the " +
      "role of its instance.";
  }
  return data.key_set ? "A key is set. The dashboard never shows it." : "No key is set.";
}

/** The section "Language model": what answers, and the control of the local inference. */
export function ModelSection({ data, user, reload }: {
  data: ModelSettings;
  user: User | null;
  reload: () => void;
}) {
  const models = Object.entries(data.models ?? {})
    .map(([role, model]) => ({ role, model }))
    .toSorted((a, b) => a.role.localeCompare(b.role));
  return (
    <Section title="Language model">
      <div className="app-grid">
        <Tile className="app-stack-tight">
          <div className="app-row app-spread">
            <h3 className="app-heading">{data.provider}</h3>
            <Tag type="blue" size="sm">Mode: {data.mode}</Tag>
          </div>
          <p>{MODE_MEANING[data.mode] ?? "The dashboard does not know this mode."}</p>
          {data.base_url !== undefined && data.base_url !== "" && (
            <p>
              <span className="app-label">Endpoint</span>{" "}
              <span className="app-mono">{data.base_url}</span>
            </p>
          )}
          {data.region !== undefined && data.region !== "" && (
            <p>
              <span className="app-label">Region</span>{" "}
              <span className="app-mono">{data.region}</span>
            </p>
          )}
          <p>
            <span className="app-label">Key</span>{" "}
            {keyText(data)}
          </p>
          <p className="app-secondary">
            This page does not change the provider. To change it, run the setup wizard
            again: <span className="app-mono">clanker setup</span>
          </p>
        </Tile>
        <LocalTile local={data.local} user={user} reload={reload} />
      </div>
      <SimpleTable title="Model of each role" rows={models} rowId={(row) => row.role}
        columns={MODEL_COLUMNS} size="sm" empty="No model is called in this mode." />
    </Section>
  );
}

/** The settings of the control plane that a person can see and change from the dashboard. */
export function SettingsPage() {
  const { user } = useSession();
  const settings = useApi<ModelSettings>("/settings/model", {
    live: (event) => event.type.startsWith("model."),
    pollMs: 10_000,
    ignoreAsOf: true,
  });
  return (
    <>
      <PageHeader title="Settings">
        What this control plane uses to write and to explain, and the model server on this
        machine.
      </PageHeader>
      <Loaded state={settings} what="the settings">
        {(data) => <ModelSection data={data} user={user} reload={settings.reload} />}
      </Loaded>
    </>
  );
}
