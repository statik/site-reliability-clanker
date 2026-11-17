import type { ComponentType } from "react";
import { linkVertical } from "d3";
import { Tag } from "@carbon/react";
import { Compare, Fire, Time, WarningAlt } from "@carbon/icons-react";
import type { Topology, TopologyEdge } from "@/api/types";
import { IconTag, StatusTag } from "@/components/status";
import { COMPACT, FULL, layoutTopology } from "@/lib/topologyLayout";
import type { Dimensions, PlacedNode, TopologyLayout } from "@/lib/topologyLayout";

type Point = [number, number];

interface BadgeLook {
  type: "purple" | "magenta" | "red" | "gray";
  icon: ComponentType;
}

const BADGES: Record<string, BadgeLook> = {
  "pending change": { type: "purple", icon: Time },
  drift: { type: "magenta", icon: WarningAlt },
  incident: { type: "red", icon: Fire },
};
const LABEL_SPACE = 20;
const SAME_ROW_DIP = 22;

function NodeCard({ placed, size, changes }: {
  placed: PlacedNode;
  size: Dimensions;
  changes: string[] | undefined;
}) {
  const node = placed.node;
  const changed = changes !== undefined;
  return (
    <div className={`topo-node${changed ? " topo-node-changed" : ""}`}
      style={{ left: placed.x, top: placed.y, width: size.nodeWidth, height: size.nodeHeight }}
      title={changed ? `${node.detail}. ${changes.join(". ")}` : node.detail}>
      <p className="topo-node-label">{node.label}</p>
      {size.labelWidth > 0 && <p className="topo-node-detail">{node.detail}</p>}
      <div className="topo-node-tags">
        {node.version !== "" && (
          <Tag type="outline" size="sm">v{node.version.replace(/^v/, "")}</Tag>
        )}
        <StatusTag status={node.status} size="sm" />
        {node.badges.map((badge) => {
          const look = BADGES[badge] ?? { type: "gray" as const, icon: WarningAlt };
          return (
            <IconTag key={badge} type={look.type} size="sm" icon={look.icon}>{badge}</IconTag>
          );
        })}
        {changed && <IconTag type="blue" size="sm" icon={Compare}>Differs</IconTag>}
      </div>
    </div>
  );
}

function edgePath(edge: TopologyEdge, layout: TopologyLayout): string | null {
  const { nodeWidth, nodeHeight } = layout.dimensions;
  const from = layout.nodes.find((placed) => placed.node.id === edge.from);
  const to = layout.nodes.find((placed) => placed.node.id === edge.to);
  if (from === undefined || to === undefined) {
    return null;
  }
  if (from.y === to.y) {
    const y = from.y + nodeHeight;
    const x0 = from.x + nodeWidth / 2;
    const x1 = to.x + nodeWidth / 2;
    return `M${x0} ${y} C${x0} ${y + SAME_ROW_DIP}, ${x1} ${y + SAME_ROW_DIP}, ${x1} ${y}`;
  }
  const upper = from.y < to.y ? from : to;
  const lower = upper === from ? to : from;
  const source: Point = [upper.x + nodeWidth / 2, upper.y + nodeHeight];
  const target: Point = [lower.x + nodeWidth / 2, lower.y];
  return linkVertical<{ source: Point; target: Point }, Point>()({ source, target });
}

/**
 * One environment drawn by layer. The edges are an SVG underlay drawn with D3 link paths and
 * the nodes are positioned cards, both styled with Carbon tokens only.
 */
export function TopologyDiagram({ topology, changes, compact = false }: {
  topology: Topology;
  changes?: Map<string, string[]>;
  compact?: boolean;
}) {
  const size = compact ? COMPACT : FULL;
  const layout = layoutTopology(topology.layers, topology.nodes, size);
  return (
    <div className="topo-scroll">
      <div className="topo-canvas" role="group" aria-label={`Topology of ${topology.env}`}
        style={{ width: layout.width, height: layout.height, marginBlockStart: LABEL_SPACE }}>
        <svg className="topo-edges" width={layout.width} height={layout.height}
          aria-hidden="true">
          {topology.edges.map((edge) => {
            const path = edgePath(edge, layout);
            return path === null ? null
              : <path key={`${edge.from}-${edge.to}`} className="topo-edge" d={path} />;
          })}
        </svg>
        {layout.rows.map((row) => (
          <p key={row.layer} className="topo-layer"
            style={size.labelWidth > 0 ? { top: row.y, width: size.labelWidth }
              : { top: row.y - LABEL_SPACE }}>
            {row.layer}{row.empty ? ": not deployed" : ""}
          </p>
        ))}
        {layout.nodes.map((placed) => (
          <NodeCard key={placed.node.id} placed={placed} size={size}
            changes={changes?.get(placed.node.id)} />
        ))}
      </div>
    </div>
  );
}
