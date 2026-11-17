// The opening scene: the six layers of the platform on each of the four targets. The
// names come from docs/PLAN.md, sections 4.1 (the contract) and 4.2 (the layers), and from
// platform/stacks/.

import { onBeat } from "./scene.js";

const TARGETS = [
  { name: "AWS", status: "validated, not applied" },
  { name: "Google Cloud", status: "validated, not applied" },
  { name: "Azure", status: "validated, not applied" },
  { name: "Local", status: "runs end to end" },
];

// From 00-bootstrap up. Each cell is [main line, second line] for one target. `flagAt` is
// when the flag shows, after the sentence about the three kinds of work starts: set it to
// the start of that part of the sentence in the voice of the build.
const LAYERS = [
  {
    name: "00-bootstrap", note: "changes rarely · high risk",
    cells: [
      ["S3 state backend", "evidence bucket · agent identities"],
      ["Cloud Storage state backend", "evidence bucket · agent identities"],
      ["Blob Storage state backend", "evidence store · agent identities"],
      ["local state backend", "evidence store"],
    ],
  },
  {
    name: "10-network", note: "changes rarely · high risk",
    cells: [
      ["VPC · subnets · NAT", "private endpoints · registry"],
      ["VPC · subnets · NAT", "private endpoints · registry"],
      ["VNet · subnets · NAT", "private endpoints · registry"],
      ["Docker network", "registry"],
    ],
  },
  {
    name: "20-cluster", note: "changes monthly · medium risk",
    flag: "Kubernetes: end of support", flagAt: "0.3s",
    cells: [
      ["EKS", "node pools · add-ons"],
      ["GKE", "node pools · add-ons"],
      ["AKS", "node pools · add-ons"],
      ["kind", "cluster nodes"],
    ],
  },
  {
    name: "30-data", note: "changes monthly · high risk",
    flag: "Postgres: patch", flagAt: "4.7s",
    cells: [
      ["RDS for PostgreSQL", "EFS · S3 backup bucket"],
      ["Cloud SQL for PostgreSQL", "Filestore · backup bucket"],
      ["Azure Database for PostgreSQL", "Azure Files · Blob Storage"],
      ["Postgres in the cluster", "shared volume · retention"],
    ],
  },
  {
    name: "40-platform", note: "changes monthly · medium risk",
    cells: [
      ["Envoy Gateway · Gateway API", "NLB · secrets"],
      ["Envoy Gateway · Gateway API", "Cloud Load Balancing · secrets"],
      ["Envoy Gateway · Gateway API", "Azure Load Balancer · secrets"],
      ["Envoy Gateway · Gateway API", "cloud-provider-kind · secrets"],
    ],
  },
  {
    name: "50-app", note: "changes weekly · low to medium risk",
    flag: "Base image: vulnerability", flagAt: "2.8s",
    cells: [
      ["Paperboat Helm release", "configuration"],
      ["Paperboat Helm release", "configuration"],
      ["Paperboat Helm release", "configuration"],
      ["Paperboat Helm release", "configuration"],
    ],
  },
];

function element(className, text = "") {
  const made = document.createElement("div");
  made.className = className;
  made.textContent = text;
  return made;
}

function header(matrix) {
  matrix.append(element("matrix-corner appear", "6 layers × 4 targets"));
  TARGETS.forEach((target, col) => {
    const cell = element("target appear");
    cell.style.setProperty("--col", String(col + 1));
    cell.append(element("target-name", target.name), element("target-status", target.status));
    matrix.append(cell);
  });
}

function layerRow(matrix, layer, row) {
  const label = element("layer appear");
  label.style.setProperty("--row", String(row));
  label.append(element("layer-name", layer.name), element("layer-note", layer.note));
  if (layer.flag) {
    const flag = element("layer-flag", layer.flag);
    flag.style.setProperty("--flag-at", layer.flagAt);
    label.append(flag);
  }
  matrix.append(label);
  layer.cells.forEach(([main, rest], col) => {
    const cell = element(`cell appear${layer.flag ? " is-flagged" : ""}`);
    cell.dataset.row = String(row);
    cell.style.setProperty("--row", String(row));
    cell.style.setProperty("--col", String(col + 1));
    if (layer.flag) cell.style.setProperty("--flag-at", layer.flagAt);
    cell.append(element("cell-main", main), element("cell-rest", rest));
    matrix.append(cell);
  });
}

const matrix = document.getElementById("matrix");
header(matrix);
// The top row of the picture is the top layer.
for (let row = LAYERS.length - 1; row >= 0; row -= 1) layerRow(matrix, LAYERS[row], row);
onBeat();
