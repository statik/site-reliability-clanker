import { useState } from "react";
import { Button, InlineNotification, Modal, ProgressBar, TextArea, TextInput, Tile }
  from "@carbon/react";
import { apiPost, errorMessage } from "@/api/client";
import { ACTION_LABEL, buildDecision, validApprovers, waitNote } from "@/api/decision";
import type { DecisionAction, DecisionRequest } from "@/api/decision";
import type { WorkItem, WorkItemDetail } from "@/api/types";
import { AiMark, ErrorNote } from "@/components/common";
import { stateLabel } from "@/components/status";
import { formatTime, toLocalInput } from "@/lib/time";
import { useServerNow } from "@/state/clock";
import { UnverifiedTag } from "@/components/Unverified";
import { useSession } from "@/state/session";

interface Pending {
  action: DecisionAction;
  request: DecisionRequest;
}

function ApproverProgress({ detail }: { detail: WorkItemDetail }) {
  const names = validApprovers(detail.work_item, detail.approvals ?? []);
  const needed = detail.approvers_required;
  const { users } = useSession();
  if (needed === 0) {
    return <p>Policy requires no approver for this tier.</p>;
  }
  const shown = names.map((name) =>
    users.some((user) => user.name === name && user.self_declared) ? `${name} (unverified)` : name);
  const helper = shown.length === 0 ? "No approver yet" : `Approved by ${shown.join(", ")}`;
  return (
    <ProgressBar label={`${Math.min(names.length, needed)} of ${needed} approvers`}
      helperText={helper} value={Math.min(names.length, needed)} max={needed}
      status={names.length >= needed ? "finished" : "active"} />
  );
}

function ConfirmModal({ pending, workItem, busy, onClose, onConfirm }: {
  pending: Pending | null;
  workItem: WorkItem;
  busy: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { user, userName } = useSession();
  const body = pending?.request.body;
  return (
    <Modal open={pending !== null} size="sm" danger={pending?.action === "reject"}
      modalLabel={`${workItem.id} · ${workItem.title}`}
      modalHeading={pending === null ? "" : `${ACTION_LABEL[pending.action]}?`}
      primaryButtonText={pending === null ? "Confirm" : ACTION_LABEL[pending.action]}
      secondaryButtonText="Cancel" primaryButtonDisabled={busy}
      onRequestClose={onClose} onRequestSubmit={onConfirm}>
      <div className="app-stack-tight">
        <p>
          You decide as {user?.display ?? userName} ({user?.role ?? "unknown role"})
          {user?.self_declared === true && <> <UnverifiedTag /></>}. The
          decision is recorded in the audit log against commit{" "}
          <span className="app-mono">{workItem.commit.slice(0, 10) || "none"}</span> and the
          hashes of the saved plans.
        </p>
        {body !== undefined && body.scheduled_for !== "" && (
          <p>The change will wait for the window at {formatTime(body.scheduled_for)}.</p>
        )}
        {body !== undefined && body.comment !== "" && <p>Comment: {body.comment}</p>}
      </div>
    </Modal>
  );
}

const BUTTON_KIND = {
  approve: "primary",
  schedule: "secondary",
  request_changes: "tertiary",
  reject: "danger",
} as const;

/** Section 9 of the approval page: the decision, with a confirmation step. */
export function Decide({ detail, onDecided }: { detail: WorkItemDetail; onDecided: () => void }) {
  const item = detail.work_item;
  const now = useServerNow();
  const { user, asOf } = useSession();
  const [comment, setComment] = useState("");
  const [schedule, setSchedule] = useState("");
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const waiting = item.state === "AwaitingApproval";
  const scheduled = item.state === "Scheduled";
  const note = waitNote(item);

  const start = (action: DecisionAction) => {
    setDone(null);
    try {
      setPending({ action, request: buildDecision(item.id, action, comment, schedule) });
      setError(null);
    } catch (problem) {
      setError(errorMessage(problem));
    }
  };
  const confirm = () => {
    if (pending === null) {
      return;
    }
    setBusy(true);
    apiPost<WorkItem>(pending.request.path, pending.request.body).then(
      (result) => {
        setDone(`Recorded: ${ACTION_LABEL[pending.action]}. State: ${stateLabel(result.state)}.`);
        setComment("");
        onDecided();
      },
      (failure: unknown) => setError(errorMessage(failure)),
    ).finally(() => {
      setBusy(false);
      setPending(null);
    });
  };

  return (
    <Tile className="app-stack">
      <ApproverProgress detail={detail} />
      {item.explanation.if_deferred !== "" && (
        <div className="app-row">
          <AiMark source={item.explanation.source} what="consequence of deferring" />
          <p className="app-fill">
            <strong>If you leave it: </strong>{item.explanation.if_deferred}
          </p>
        </div>
      )}
      {scheduled && (
        <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
          title={`${note.charAt(0).toUpperCase()}${note.slice(1)}.`}
          subtitle={"To cancel the apply, an approver or an admin can reject this work item. " +
            "The server refuses the other decisions."} />
      )}
      {!waiting && !scheduled && (
        <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
          title={`This work item is ${stateLabel(item.state)}.`}
          subtitle="It does not wait for a decision, so the server will refuse one." />
      )}
      {waiting && user?.role === "viewer" && (
        <InlineNotification kind="warning" lowContrast hideCloseButton className="app-note"
          title="You are signed in as a viewer."
          subtitle="Only an approver or an admin can decide. The server refuses a viewer." />
      )}
      {asOf !== null && (
        <InlineNotification kind="warning" lowContrast hideCloseButton className="app-note"
          title="You are viewing a past time." subtitle="A decision acts on the live state." />
      )}
      <TextArea id="decision-comment" labelText="Comment" rows={3} value={comment}
        helperText="Required to request changes or to reject. Recorded with the decision."
        onChange={(event) => setComment(event.target.value)} />
      <TextInput id="decision-schedule" type="datetime-local" value={schedule}
        labelText="Maintenance window (for approve and schedule)" min={toLocalInput(now)}
        helperText={`Your local zone. The server's time now is ${formatTime(now)}.`}
        onChange={(event) => setSchedule(event.target.value)} />
      {error !== null && <ErrorNote title="The decision was not recorded." message={error} />}
      {done !== null && (
        <InlineNotification kind="success" lowContrast hideCloseButton className="app-note"
          title={done} />
      )}
      <div className="app-row">
        {(Object.keys(ACTION_LABEL) as DecisionAction[]).map((action) => (
          <Button key={action} kind={BUTTON_KIND[action]} disabled={busy}
            onClick={() => start(action)}>
            {ACTION_LABEL[action]}
          </Button>
        ))}
      </div>
      <ConfirmModal pending={pending} workItem={item} busy={busy}
        onClose={() => setPending(null)} onConfirm={confirm} />
    </Tile>
  );
}
