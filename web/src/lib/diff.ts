export interface FileDiff {
  path: string;
  added: number;
  removed: number;
  text: string;
}

const FILE_HEADER = /^diff --git a\/(.+) b\/(.+)$/;

/** Splits the output of `git diff` into one entry per file, with line counts. */
export function splitDiff(diff: string): FileDiff[] {
  const files: FileDiff[] = [];
  let current: { path: string; lines: string[] } | null = null;
  const flush = () => {
    if (current === null) {
      return;
    }
    const body = current.lines.filter((line) => !line.startsWith("+++") &&
      !line.startsWith("---"));
    files.push({
      path: current.path,
      added: body.filter((line) => line.startsWith("+")).length,
      removed: body.filter((line) => line.startsWith("-")).length,
      text: current.lines.join("\n"),
    });
  };
  for (const line of diff.split("\n")) {
    const header = FILE_HEADER.exec(line);
    if (header !== null) {
      flush();
      current = { path: header[2] ?? header[1] ?? "", lines: [] };
    }
    current?.lines.push(line);
  }
  flush();
  return files;
}
