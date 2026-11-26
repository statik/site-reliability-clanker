import { useState } from "react";
import {
  Button,
  InlineNotification,
  StructuredListBody,
  StructuredListCell,
  StructuredListHead,
  StructuredListRow,
  StructuredListWrapper,
} from "@carbon/react";
import { Stamp, Undo } from "@carbon/icons-react";
import { apiDelete, apiPost, errorMessage } from "@/api/client";
import type { Runbook, StandingApproval, User } from "@/api/types";
import { ErrorNote, Loaded, PageHeader, ScrollRegion } from "@/components/common";
import { StatusTag, TierTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { formatTime } from "@/lib/time";
import { useSession } from "@/state/session";

/** The only tier that a standing approval covers, as runbook.StandingTier on the server. */
const STANDING_TIER = "T1";

export interface StandingControl {
  sign: boolean;
  revoke: boolean;
  /** Why a control is off, or "" when nothing is off. */
  reason: string;
}

/**
 * Says whether this user can sign or revoke the standing approval of a runbook, and when
 * not, why. The server checks the same rules.
 */
export function standingControl(user: User | null, runbook: Runbook): StandingControl {
  const signed = runbook.standing_approver !== "";
  if (user?.role !== "admin" && user?.role !== "approver") {
    return { sign: false, revoke: false,
      reason: `Only an approver or an admin signs or revokes a standing approval. You are ` +
        `${user?.display ?? "a user"} (${user?.role ?? "unknown role"}).` };
  }
  if (runbook.tier !== STANDING_TIER) {
    return { sign: false, revoke: signed,
      reason: `This runbook needs tier ${runbook.tier}. A standing approval covers only ` +
        `tier ${STANDING_TIER}.` };
  }
  return { sign: true, revoke: signed, reason: "" };
}

/** Who signed the standing approval of a runbook and when, in one sentence. */
export function signature(runbook: Runbook, users: User[]): string {
  if (runbook.standing_approver === "") {
    return "Nobody signed a standing approval. The runbook cannot run.";
  }
  const signer = users.find((user) => user.name === runbook.standing_approver);
  const name = signer?.display ?? runbook.standing_approver;
  if (runbook.standing_signed_at === "") {
    return `Signed by ${name}.`;
  }
  return `Signed by ${name} on ${formatTime(runbook.standing_signed_at)}.`;
}

function RunbookRow({ runbook, user, users, busy, act }: {
  runbook: Runbook;
  user: User | null;
  users: User[];
  busy: boolean;
  act: (runbook: Runbook, action: "sign" | "revoke") => void;
}) {
  const control = standingControl(user, runbook);
  const signed = runbook.standing_approver !== "";
  return (
    <StructuredListRow>
      <StructuredListCell>
        <p>{runbook.title ?? runbook.name}</p>
        <p className="app-mono app-secondary">{runbook.name}</p>
      </StructuredListCell>
      <StructuredListCell noWrap><TierTag tier={runbook.tier} size="sm" /></StructuredListCell>
      <StructuredListCell>{runbook.description}</StructuredListCell>
      <StructuredListCell>
        <StatusTag size="sm" status={signed ? "active" : "not deployed"}
          label={signed ? "Signed" : "Not signed"} />
        <p>{signature(runbook, users)}</p>
        {control.reason !== "" && <p className="app-secondary">{control.reason}</p>}
      </StructuredListCell>
      <StructuredListCell noWrap>
        <div className="app-row">
          <Button size="sm" renderIcon={Stamp} disabled={!control.sign || busy}
            onClick={() => act(runbook, "sign")}>
            {signed ? "Sign again" : "Sign"}
          </Button>
          <Button size="sm" kind="tertiary" renderIcon={Undo} disabled={!control.revoke || busy}
            onClick={() => act(runbook, "revoke")}>
            Revoke
          </Button>
        </div>
      </StructuredListCell>
    </StructuredListRow>
  );
}

/** The runbooks with their standing approvals, and the controls to sign and to revoke. */
export function RunbookList({ runbooks, user, users, reload }: {
  runbooks: Runbook[];
  user: User | null;
  users: User[];
  reload: () => void;
}) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const act = (runbook: Runbook, action: "sign" | "revoke") => {
    const path = `/runbooks/${runbook.name}/standing-approval`;
    setBusy(runbook.name);
    setError(null);
    setDone(null);
    const request = action === "sign" ? apiPost<StandingApproval>(path, {})
      : apiDelete<StandingApproval>(path);
    request.then(
      () => setDone(action === "sign"
        ? `You signed the standing approval of ${runbook.name}.`
        : `You revoked the standing approval of ${runbook.name}.`),
      (failure: unknown) => setError(errorMessage(failure)),
    ).finally(() => {
      setBusy(null);
      reload();
    });
  };
  return (
    <div className="app-stack">
      {error !== null && (
        <ErrorNote title="The standing approval did not change." message={error} />
      )}
      {done !== null && (
        <InlineNotification kind="success" lowContrast hideCloseButton className="app-note"
          title="Done." subtitle={done} />
      )}
      <ScrollRegion label="Runbooks">
        <StructuredListWrapper aria-label="Runbooks">
          <StructuredListHead>
            <StructuredListRow head>
              <StructuredListCell head>Runbook</StructuredListCell>
              <StructuredListCell head>Tier</StructuredListCell>
              <StructuredListCell head>What it does</StructuredListCell>
              <StructuredListCell head>Standing approval</StructuredListCell>
              <StructuredListCell head>Change</StructuredListCell>
            </StructuredListRow>
          </StructuredListHead>
          <StructuredListBody>
            {runbooks.map((runbook) => (
              <RunbookRow key={runbook.name} runbook={runbook} user={user} users={users}
                busy={busy !== null} act={act} />
            ))}
          </StructuredListBody>
        </StructuredListWrapper>
      </ScrollRegion>
    </div>
  );
}

/** The runbooks of the runbooks repository and who approved their runs. */
export function RunbooksPage() {
  const { user, users } = useSession();
  const runbooks = useApi<Runbook[]>("/runbooks", {
    live: (event) => event.type.startsWith("standing_approval."),
    ignoreAsOf: true,
  });
  return (
    <>
      <PageHeader title="Runbooks">
        A runbook is a reviewed job from the runbooks repository. It runs only under a
        standing approval: an approver or an admin signs it once, and it covers every later
        run until somebody revokes it. Each run still gets its own short-lived grant.
      </PageHeader>
      <Loaded state={runbooks} what="the runbooks">
        {(data) => <RunbookList runbooks={data} user={user} users={users}
          reload={runbooks.reload} />}
      </Loaded>
    </>
  );
}
