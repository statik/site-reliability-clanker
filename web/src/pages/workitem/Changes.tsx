import type { ComponentType } from "react";
import {
  Accordion,
  AccordionItem,
  InlineNotification,
  ListItem,
  StructuredListBody,
  StructuredListCell,
  StructuredListHead,
  StructuredListRow,
  StructuredListWrapper,
  Tile,
  UnorderedList,
} from "@carbon/react";
import { Add, Edit, Renew, TrashCan, View } from "@carbon/icons-react";
import type { Plan, ResourceChange } from "@/api/types";
import { IconTag } from "@/components/status";
import { ScrollRegion } from "@/components/common";

type TagType = "green" | "blue" | "red" | "magenta" | "gray";

interface ActionLook {
  type: TagType;
  icon: ComponentType;
  label: string;
}

const ACTIONS: Record<string, ActionLook> = {
  create: { type: "green", icon: Add, label: "Create" },
  update: { type: "blue", icon: Edit, label: "Change" },
  delete: { type: "red", icon: TrashCan, label: "Destroy" },
  replace: { type: "magenta", icon: Renew, label: "Replace" },
  read: { type: "gray", icon: View, label: "Read" },
};

function ActionTag({ action }: { action: string }) {
  const look = ACTIONS[action] ?? { type: "gray" as const, icon: View, label: action };
  return <IconTag type={look.type} icon={look.icon} size="sm">{look.label}</IconTag>;
}

function countsText(plan: Plan): string {
  const { add, change, destroy, replace } = plan.facts;
  return `${add} to create, ${change} to change, ${destroy} to destroy, ${replace} to replace`;
}

function ResourceList({ resources }: { resources: ResourceChange[] }) {
  return (
    <ScrollRegion label="Resources in this plan">
      <StructuredListWrapper isCondensed aria-label="Resources in this plan">
        <StructuredListHead>
          <StructuredListRow head>
            <StructuredListCell head>Action</StructuredListCell>
            <StructuredListCell head>Resource</StructuredListCell>
            <StructuredListCell head>What changes</StructuredListCell>
          </StructuredListRow>
        </StructuredListHead>
        <StructuredListBody>
          {resources.map((resource) => (
            <StructuredListRow key={resource.address}>
              <StructuredListCell><ActionTag action={resource.action} /></StructuredListCell>
              <StructuredListCell>
                <span className="app-mono">{resource.address}</span>
              </StructuredListCell>
              <StructuredListCell>
                {(resource.details ?? []).length === 0
                  ? "New resource" : (
                    <UnorderedList>
                      {(resource.details ?? []).map((detail) => (
                        <ListItem key={detail}>{detail}</ListItem>
                      ))}
                    </UnorderedList>
                  )}
              </StructuredListCell>
            </StructuredListRow>
          ))}
        </StructuredListBody>
      </StructuredListWrapper>
    </ScrollRegion>
  );
}

function DestructiveCallout({ plans }: { plans: Plan[] }) {
  const destructive = plans.flatMap((plan) =>
    (plan.facts.resources ?? [])
      .filter((resource) => resource.action === "delete" || resource.action === "replace")
      .map((resource) => ({ layer: plan.layer, resource })));
  if (destructive.length === 0) {
    return (
      <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
        title="Nothing is destroyed or replaced."
        subtitle="The saved plans only create or change resources in place." />
    );
  }
  return (
    <InlineNotification kind="warning" lowContrast hideCloseButton className="app-note"
      title={`${destructive.length} resources are destroyed or replaced.`}>
      <UnorderedList>
        {destructive.map(({ layer, resource }) => (
          <ListItem key={`${layer}/${resource.address}`}>
            {resource.action === "delete" ? "Destroy" : "Replace"} {resource.address} in
            layer {layer}
          </ListItem>
        ))}
      </UnorderedList>
    </InlineNotification>
  );
}

function WarningsCallout({ plans }: { plans: Plan[] }) {
  const warnings = plans.flatMap((plan) =>
    (plan.facts.warnings ?? []).map((warning) => ({ layer: plan.layer, warning })));
  if (warnings.length === 0) {
    return null;
  }
  return (
    <InlineNotification kind="warning" lowContrast hideCloseButton className="app-note"
      title="Read this before you decide.">
      <UnorderedList>
        {warnings.map(({ layer, warning }) => (
          <ListItem key={`${layer}/${warning}`}>Layer {layer}: {warning}</ListItem>
        ))}
      </UnorderedList>
    </InlineNotification>
  );
}

/** Section 4 of the approval page: what changes, per layer, from the plan itself. */
export function Changes({ plans, deferred }: { plans: Plan[]; deferred: string[] }) {
  const current = plans.filter((plan) => !plan.superseded);
  if (current.length === 0 && deferred.length === 0) {
    return <Tile>No plan is saved for this work item, so it changes no managed resource.</Tile>;
  }
  return (
    <div className="app-stack-tight">
      <WarningsCallout plans={current} />
      <DestructiveCallout plans={current} />
      <Accordion align="start">
        {current.map((plan) => (
          <AccordionItem key={plan.id} open={current.length === 1}
            title={`Layer ${plan.layer}: ${countsText(plan)}`}>
            <ResourceList resources={plan.facts.resources ?? []} />
            {(plan.facts.outputs ?? []).length > 0 && (
              <p className="app-label">
                Outputs that change: {(plan.facts.outputs ?? []).join(", ")}
              </p>
            )}
          </AccordionItem>
        ))}
        {deferred.map((layer) => (
          <AccordionItem key={layer} title={`Layer ${layer}: planned at apply time`}>
            <p>
              This layer depends on outputs of an earlier layer, so it is planned at apply
              time. Its plan must only create resources. If it would change, replace or
              destroy anything, policy refuses it and the apply stops.
            </p>
          </AccordionItem>
        ))}
      </Accordion>
    </div>
  );
}
