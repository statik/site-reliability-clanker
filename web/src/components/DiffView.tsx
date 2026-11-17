import { Accordion, AccordionItem, CodeSnippet } from "@carbon/react";
import { Add, Subtract } from "@carbon/icons-react";
import { IconTag } from "@/components/status";
import { splitDiff } from "@/lib/diff";

/** Shows a `git diff`, one collapsible file at a time, with the count of lines on each side. */
export function DiffView({ diff }: { diff: string }) {
  const files = splitDiff(diff);
  if (files.length === 0) {
    return <p className="app-secondary">The two commits have the same content.</p>;
  }
  return (
    <Accordion align="start">
      {files.map((file) => (
        <AccordionItem key={file.path} open={files.length <= 3} title={
          <span className="app-row">
            <span className="app-mono">{file.path}</span>
            <IconTag type="green" size="sm" icon={Add}>{file.added} added</IconTag>
            <IconTag type="red" size="sm" icon={Subtract}>{file.removed} removed</IconTag>
          </span>
        }>
          <CodeSnippet type="multi" feedback="Copied" className="app-full"
            aria-label={`Diff of ${file.path}`} maxCollapsedNumberOfRows={30}>
            {file.text}
          </CodeSnippet>
        </AccordionItem>
      ))}
    </Accordion>
  );
}
