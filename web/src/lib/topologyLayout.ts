import type { Topology, TopologyNode } from "@/api/types";

const HIDDEN_LAYER = "00-bootstrap";

export interface Dimensions {
  nodeWidth: number;
  nodeHeight: number;
  columnGap: number;
  rowGap: number;
  /** The width of the column of layer names. Zero puts each name above its row. */
  labelWidth: number;
}

export const FULL: Dimensions = { nodeWidth: 184, nodeHeight: 128, columnGap: 24, rowGap: 56,
  labelWidth: 120 };
/** Narrow enough for two diagrams side by side. */
export const COMPACT: Dimensions = { nodeWidth: 122, nodeHeight: 112, columnGap: 8,
  rowGap: 56, labelWidth: 0 };

export interface PlacedNode {
  node: TopologyNode;
  x: number;
  y: number;
}

export interface LayerRow {
  layer: string;
  y: number;
  empty: boolean;
}

export interface TopologyLayout {
  nodes: PlacedNode[];
  rows: LayerRow[];
  width: number;
  height: number;
  dimensions: Dimensions;
}

/**
 * Places the nodes in one row per layer. The application layer is the top row and the network
 * layer is the bottom row, and each row is centred.
 */
export function layoutTopology(
  layers: string[],
  nodes: TopologyNode[],
  dimensions: Dimensions = FULL,
): TopologyLayout {
  const { nodeWidth, nodeHeight, columnGap, rowGap, labelWidth } = dimensions;
  const shown = layers.filter((layer) => layer !== HIDDEN_LAYER).toReversed();
  const widest = Math.max(1, ...shown.map((layer) =>
    nodes.filter((node) => node.layer === layer).length));
  const inner = widest * nodeWidth + (widest - 1) * columnGap;
  const placed: PlacedNode[] = [];
  const rows: LayerRow[] = [];
  shown.forEach((layer, index) => {
    const y = index * (nodeHeight + rowGap);
    const members = nodes.filter((node) => node.layer === layer);
    rows.push({ layer, y, empty: members.length === 0 });
    const rowWidth = members.length * nodeWidth + (members.length - 1) * columnGap;
    const start = labelWidth + (inner - rowWidth) / 2;
    members.forEach((node, column) => {
      placed.push({ node, x: start + column * (nodeWidth + columnGap), y });
    });
  });
  const height = shown.length * nodeHeight + Math.max(shown.length - 1, 0) * rowGap;
  return { nodes: placed, rows, width: labelWidth + inner, height, dimensions };
}

/**
 * Lists, for each node, how it differs between two topologies. A node that is the same in both
 * has no entry.
 */
export function diffTopology(then: Topology, now: Topology): Map<string, string[]> {
  const differences = new Map<string, string[]>();
  const before = new Map(then.nodes.map((node) => [node.id, node]));
  const after = new Map(now.nodes.map((node) => [node.id, node]));
  for (const id of new Set([...before.keys(), ...after.keys()])) {
    const old = before.get(id);
    const current = after.get(id);
    const reasons: string[] = [];
    if (old === undefined) {
      reasons.push("Did not exist then");
    } else if (current === undefined) {
      reasons.push("Does not exist now");
    } else {
      if (old.version !== current.version) {
        reasons.push(`Version ${old.version || "none"} then, ${current.version || "none"} now`);
      }
      if (old.status !== current.status) {
        reasons.push(`Status ${old.status} then, ${current.status} now`);
      }
      const oldBadges = old.badges.toSorted().join(", ");
      const newBadges = current.badges.toSorted().join(", ");
      if (oldBadges !== newBadges) {
        reasons.push(`Badges ${oldBadges || "none"} then, ${newBadges || "none"} now`);
      }
    }
    if (reasons.length > 0) {
      differences.set(id, reasons);
    }
  }
  return differences;
}
