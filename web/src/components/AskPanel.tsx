import { useState } from "react";
import { Button, InlineLoading, Tag, TextInput, Tile } from "@carbon/react";
import { Send } from "@carbon/icons-react";
import { apiPost, errorMessage } from "@/api/client";
import type { AgentCall, ChatEntry, Citation } from "@/api/types";
import { query } from "@/api/urls";
import { AiMark, AppLink, ErrorNote, Loaded } from "@/components/common";
import { useApi } from "@/hooks/useApi";
import { formatTime } from "@/lib/time";

function citationTarget(citation: Citation): string | null {
  switch (citation.type) {
    case "work_item":
      return `/work/${citation.id}`;
    case "event":
      return `/audit?seq=${citation.id}`;
    case "grant":
      return `/access?grant=${citation.id}`;
    case "pull":
      return `/code/pulls/${citation.id}`;
    default:
      return null;
  }
}

function Citations({ citations }: { citations: Citation[] }) {
  if (citations.length === 0) {
    return null;
  }
  return (
    <div className="app-row" aria-label="Sources">
      <span className="app-label">Sources</span>
      {citations.map((citation) => {
        const target = citationTarget(citation);
        const key = `${citation.type}/${citation.id}`;
        return target === null
          ? <Tag key={key} type="outline" size="sm">{citation.label}</Tag>
          : <AppLink key={key} to={target}>{citation.label}</AppLink>;
      })}
    </div>
  );
}

function Message({ entry, source }: { entry: ChatEntry; source: string }) {
  if (entry.role === "user") {
    return (
      <Tile className="app-chat-user">
        <p className="app-label">{entry.user} · {formatTime(entry.at)}</p>
        <p>{entry.body}</p>
      </Tile>
    );
  }
  return (
    <Tile className="app-chat-answer app-stack-tight" decorator={
      <AiMark source={source} what="answer" />
    }>
      <p className="app-label">S.R.C. · {formatTime(entry.at)}</p>
      <p className="app-prose">{entry.body}</p>
      <Citations citations={entry.citations ?? []} />
    </Tile>
  );
}

/** The model or recording that most recently answered, because answers carry no source. */
function useAnswerSource(workItem: string): string {
  const calls = useApi<AgentCall[]>(`/agent/calls${query({ work_item: workItem, limit: 20 })}`,
    { live: (event) => event.type === "agent.answered" });
  const call = (calls.data ?? []).find((candidate) => candidate.role === "explainer");
  if (call === undefined) {
    return "";
  }
  return call.mode === "replay" ? "replay" : call.model;
}

/** A chat grounded in the control plane. With a work item it is scoped to that item. */
export function AskPanel({ workItem, suggestions }: { workItem: string; suggestions: string[] }) {
  const path = `/chat${query({ work_item: workItem })}`;
  const history = useApi<ChatEntry[]>(path, {
    live: (event) => event.type === "agent.answered" && (event.work_item ?? "") === workItem,
  });
  const source = useAnswerSource(workItem);
  const [question, setQuestion] = useState("");
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const ask = (text: string) => {
    const trimmed = text.trim();
    if (trimmed === "" || pending !== null) {
      return;
    }
    setPending(trimmed);
    setError(null);
    apiPost<ChatEntry>("/chat", { work_item: workItem, question: trimmed })
      .then(() => setQuestion(""), (failure: unknown) => setError(errorMessage(failure)))
      .finally(() => {
        setPending(null);
        history.reload();
      });
  };

  return (
    <div className="app-stack">
      <div className="app-row">
        {suggestions.map((suggestion) => (
          <Button key={suggestion} kind="tertiary" size="sm" disabled={pending !== null}
            onClick={() => ask(suggestion)}>
            {suggestion}
          </Button>
        ))}
      </div>
      <Loaded state={history} what="the conversation">
        {(entries) => (
          <div className="app-chat" aria-live="polite">
            {entries.length === 0 && pending === null && (
              <p className="app-secondary">No question has been asked here yet.</p>
            )}
            {entries.map((entry) => <Message key={entry.id} entry={entry} source={source} />)}
          </div>
        )}
      </Loaded>
      {pending !== null && <InlineLoading description={`Answering: ${pending}`} />}
      {error !== null && <ErrorNote title="The question was not answered." message={error} />}
      <form className="app-row app-row-end" onSubmit={(event) => {
        event.preventDefault();
        ask(question);
      }}>
        <div className="app-fill">
          <TextInput id={`ask-${workItem === "" ? "global" : workItem}`} value={question}
            labelText={workItem === "" ? "Ask S.R.C." : `Ask about ${workItem}`}
            placeholder="Type a question" onChange={(event) => setQuestion(event.target.value)} />
        </div>
        <Button type="submit" renderIcon={Send}
          disabled={pending !== null || question.trim() === ""}>
          Ask
        </Button>
      </form>
    </div>
  );
}
