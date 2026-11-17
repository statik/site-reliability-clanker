import { useState } from "react";
import { ActionableNotification, HeaderGlobalAction, Modal, TextInput } from "@carbon/react";
import { RecentlyViewed } from "@carbon/icons-react";
import { formatTime, toLocalInput } from "@/lib/time";
import { useServerNow } from "@/state/clock";
import { useSession } from "@/state/session";

/** The persistent banner of the point-in-time view. */
export function AsOfBanner() {
  const { asOf, setAsOf } = useSession();
  if (asOf === null) {
    return null;
  }
  return (
    <ActionableNotification kind="warning" lowContrast inline hideCloseButton
      className="app-asof-banner" title={`Viewing ${formatTime(asOf)}. Not live.`}
      subtitle="Overview, work items, topology and version posture show that moment."
      actionButtonLabel="Back to now" onActionButtonClick={() => setAsOf(null)} />
  );
}

/** The header control that sets the global "as of" time. */
export function AsOfControl() {
  const { asOf, setAsOf } = useSession();
  const now = useServerNow();
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const chosen = value === "" ? null : new Date(value);
  const invalid = chosen !== null && (Number.isNaN(chosen.getTime()) || chosen > now);
  return (
    <>
      <HeaderGlobalAction isActive={asOf !== null}
        aria-label={asOf === null ? "View as of a past time" : "Change the as-of time"}
        onClick={() => {
          setValue(toLocalInput(asOf ?? now));
          setOpen(true);
        }}>
        <RecentlyViewed size={20} />
      </HeaderGlobalAction>
      <Modal open={open} size="sm" modalHeading="View the dashboard as of a past time"
        modalLabel="Point-in-time view" primaryButtonText="View as of this time"
        secondaryButtonText={asOf === null ? "Cancel" : "Back to now"}
        primaryButtonDisabled={chosen === null || invalid}
        onRequestClose={() => setOpen(false)}
        onSecondarySubmit={() => {
          setAsOf(null);
          setOpen(false);
        }}
        onRequestSubmit={() => {
          setAsOf(chosen);
          setOpen(false);
        }}>
        <TextInput id="as-of-input" type="datetime-local" labelText="Time (your local zone)"
          value={value} max={toLocalInput(now)} invalid={invalid}
          invalidText="Choose a time in the past."
          helperText={`The server's time now is ${formatTime(now)}.`}
          onChange={(event) => setValue(event.target.value)} />
      </Modal>
    </>
  );
}
