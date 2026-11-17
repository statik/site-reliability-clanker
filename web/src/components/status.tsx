import type { ComponentType, ReactNode } from "react";
import { Tag } from "@carbon/react";
import {
  Calendar,
  CheckmarkFilled,
  CheckmarkOutline,
  CircleDash,
  Edit,
  ErrorFilled,
  InProgress,
  Misuse,
  Pending,
  Security,
  Time,
  Undo,
  UserAvatar,
  View,
  WarningAltFilled,
  WarningFilled,
} from "@carbon/icons-react";

export type TagType = "red" | "magenta" | "purple" | "blue" | "cyan" | "teal" | "green" | "gray"
  | "cool-gray" | "warm-gray" | "high-contrast" | "outline";
type Icon = ComponentType<{ size?: number | string }>;

interface Look {
  type: TagType;
  icon: Icon;
  label: string;
}

const STATE_LOOK: Record<string, Look> = {
  Detected: { type: "gray", icon: View, label: "Detected" },
  Drafting: { type: "gray", icon: Edit, label: "Drafting" },
  Planned: { type: "cool-gray", icon: CheckmarkOutline, label: "Planned" },
  AwaitingApproval: { type: "purple", icon: UserAvatar, label: "Awaiting approval" },
  ChangesRequested: { type: "magenta", icon: Edit, label: "Changes requested" },
  Approved: { type: "teal", icon: CheckmarkOutline, label: "Approved" },
  Scheduled: { type: "cyan", icon: Calendar, label: "Scheduled" },
  Applying: { type: "blue", icon: InProgress, label: "Applying" },
  Verifying: { type: "blue", icon: Pending, label: "Verifying" },
  Done: { type: "green", icon: CheckmarkFilled, label: "Done" },
  Failed: { type: "red", icon: ErrorFilled, label: "Failed" },
  RolledBack: { type: "magenta", icon: Undo, label: "Rolled back" },
  Rejected: { type: "warm-gray", icon: Misuse, label: "Rejected" },
};

const TIER_LOOK: Record<string, Look> = {
  T0: { type: "gray", icon: View, label: "T0 Read-only" },
  T1: { type: "teal", icon: CheckmarkOutline, label: "T1 Routine" },
  T2: { type: "blue", icon: Security, label: "T2 Normal change" },
  T3: { type: "red", icon: WarningAltFilled, label: "T3 High risk" },
};

const STATUS_LOOK: Record<string, Look> = {
  ok: { type: "green", icon: CheckmarkFilled, label: "Healthy" },
  current: { type: "green", icon: CheckmarkFilled, label: "Current" },
  succeeded: { type: "green", icon: CheckmarkFilled, label: "Succeeded" },
  sent: { type: "green", icon: CheckmarkFilled, label: "Sent" },
  merged: { type: "green", icon: CheckmarkFilled, label: "Merged" },
  used: { type: "gray", icon: CheckmarkOutline, label: "Used" },
  resolved: { type: "green", icon: CheckmarkFilled, label: "Resolved" },
  degraded: { type: "magenta", icon: WarningFilled, label: "Degraded" },
  behind: { type: "magenta", icon: WarningFilled, label: "Behind" },
  running: { type: "blue", icon: InProgress, label: "Running" },
  active: { type: "blue", icon: InProgress, label: "Active" },
  issued: { type: "cyan", icon: Time, label: "Issued" },
  open: { type: "purple", icon: Pending, label: "Open" },
  failed: { type: "red", icon: ErrorFilled, label: "Failed" },
  revoked: { type: "warm-gray", icon: Misuse, label: "Revoked" },
  expired: { type: "warm-gray", icon: Time, label: "Expired" },
  closed: { type: "warm-gray", icon: Misuse, label: "Closed" },
  "not deployed": { type: "outline", icon: CircleDash, label: "Not deployed" },
};

/**
 * A tag with an icon before its label. The icon is part of the content, because Carbon's small
 * tag does not draw `renderIcon`, and a status must never be colour alone.
 */
export function IconTag({ type, icon: IconComponent, size = "md", children }: {
  type: TagType;
  icon: Icon;
  size?: "sm" | "md";
  children: ReactNode;
}) {
  return (
    <Tag type={type} size={size}>
      <span className="app-tag">
        <IconComponent size={size === "sm" ? 12 : 16} />
        {children}
      </span>
    </Tag>
  );
}

function LookTag({ look, size }: { look: Look; size?: "sm" | "md" }) {
  return <IconTag type={look.type} icon={look.icon} size={size ?? "md"}>{look.label}</IconTag>;
}

function unknownLook(value: string): Look {
  return { type: "outline", icon: CircleDash, label: value === "" ? "Not set" : value };
}

/** The state of a work item, as an icon and a label. */
export function StateTag({ state, size }: { state: string; size?: "sm" | "md" }) {
  return <LookTag look={STATE_LOOK[state] ?? unknownLook(state)} {...sizeProp(size)} />;
}

export function stateLabel(state: string): string {
  return STATE_LOOK[state]?.label ?? state;
}

/** The risk tier that policy assigned, as an icon and a label. */
export function TierTag({ tier, size }: { tier: string; size?: "sm" | "md" }) {
  const look = TIER_LOOK[tier] ?? unknownLook(tier === "" ? "No tier yet" : tier);
  return <LookTag look={look} {...sizeProp(size)} />;
}

/** A generic status (health, job, grant, delivery, version), as an icon and a label. */
export function StatusTag({ status, label, size }: {
  status: string;
  label?: string;
  size?: "sm" | "md";
}) {
  const look = STATUS_LOOK[status.toLowerCase()] ?? unknownLook(status);
  const shown = label === undefined ? look : { ...look, label };
  return <LookTag look={shown} {...sizeProp(size)} />;
}

function sizeProp(size: "sm" | "md" | undefined): { size?: "sm" | "md" } {
  return size === undefined ? {} : { size };
}
