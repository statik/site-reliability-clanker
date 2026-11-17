import { Link as RouterLink, useParams, useSearchParams } from "react-router-dom";
import {
  Breadcrumb,
  BreadcrumbItem,
  ClickableTile,
  CodeSnippet,
  ContainedList,
  ContainedListItem,
  Tag,
  Tile,
} from "@carbon/react";
import { Document, Folder } from "@carbon/icons-react";
import { useNavigate } from "react-router-dom";
import type { Blob, Commit, PullRequest, Repo, TreeEntry } from "@/api/types";
import { query } from "@/api/urls";
import { AppLink, Loaded, PageHeader, Section, WorkItemLink } from "@/components/common";
import { DiffView } from "@/components/DiffView";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StatusTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { formatTime, shortHash } from "@/lib/time";

const forgeEvent = (event: { type: string }) =>
  event.type.startsWith("pr.") || event.type.startsWith("apply.") ||
  event.type.startsWith("workitem.");

function cloneUrl(repo: Repo): string {
  return `${window.location.origin}${repo.clone_url}`;
}

const PULL_COLUMNS: Column<PullRequest>[] = [
  { key: "id", header: "Pull request", value: (row) => row.id,
    render: (row) => <AppLink to={`/code/pulls/${row.id}`}>#{row.id}</AppLink> },
  { key: "title", header: "Title", value: (row) => row.title },
  { key: "state", header: "State", value: (row) => row.state,
    render: (row) => <StatusTag status={row.state} size="sm" /> },
  { key: "repo", header: "Repository", value: (row) => row.repo },
  { key: "branch", header: "Branch", value: (row) => row.branch },
  { key: "author", header: "Author", value: (row) => row.author },
  { key: "work_item", header: "Work item", value: (row) => row.work_item,
    render: (row) => <WorkItemLink id={row.work_item} /> },
  { key: "created", header: "Opened", value: (row) => row.created_at,
    render: (row) => formatTime(row.created_at) },
];

/** The forge: repositories and pull requests. */
export function CodePage() {
  const navigate = useNavigate();
  const repos = useApi<Repo[]>("/forge/repos", { live: forgeEvent });
  const pulls = useApi<PullRequest[]>("/forge/pulls", { live: forgeEvent });
  return (
    <>
      <PageHeader title="Code">
        The bundled git forge. The agent's changes arrive as pull requests on platform-live,
        and a person can push a branch to the same repositories.
      </PageHeader>
      <Section title="Repositories">
        <Loaded state={repos} what="the repositories">
          {(data) => (
            <div className="app-grid">
              {data.map((repo) => (
                <ClickableTile key={repo.name} onClick={() => navigate(`/code/${repo.name}`)}>
                  <div className="app-stack-tight">
                    <h3 className="app-heading">{repo.name}</h3>
                    <p className="app-label">
                      {repo.branches.length} branches · main at {shortHash(repo.head)}
                    </p>
                    <p className="app-label">Clone</p>
                    <p className="app-mono">git clone {cloneUrl(repo)}</p>
                  </div>
                </ClickableTile>
              ))}
            </div>
          )}
        </Loaded>
      </Section>
      <Section title="Pull requests">
        <Loaded state={pulls} what="the pull requests">
          {(data) => (
            <SimpleTable title="Pull requests" rows={data} rowId={(row) => String(row.id)}
              columns={PULL_COLUMNS} empty="No pull request has been opened." />
          )}
        </Loaded>
      </Section>
    </>
  );
}

function Crumbs({ repo, path, gitRef }: { repo: string; path: string; gitRef: string }) {
  const parts = path === "" ? [] : path.split("/");
  const to = (index: number) =>
    `/code/${repo}${query({ ref: gitRef, path: parts.slice(0, index + 1).join("/") })}`;
  return (
    <Breadcrumb noTrailingSlash>
      <BreadcrumbItem><RouterLink to="/code">Code</RouterLink></BreadcrumbItem>
      <BreadcrumbItem isCurrentPage={parts.length === 0}>
        <RouterLink to={`/code/${repo}${query({ ref: gitRef })}`}>{repo}</RouterLink>
      </BreadcrumbItem>
      {parts.map((part, index) => (
        <BreadcrumbItem key={to(index)} isCurrentPage={index === parts.length - 1}>
          <RouterLink to={to(index)}>{part}</RouterLink>
        </BreadcrumbItem>
      ))}
    </Breadcrumb>
  );
}

function Tree({ repo, dir, gitRef, selected }: {
  repo: string;
  dir: string;
  gitRef: string;
  selected: string;
}) {
  const navigate = useNavigate();
  const tree = useApi<TreeEntry[]>(
    `/forge/repos/${repo}/tree${query({ ref: gitRef, path: dir })}`, { live: forgeEvent });
  const open = (entry: TreeEntry) => navigate(`/code/${repo}${query({
    ref: gitRef, path: entry.path, ...(entry.type === "tree" ? {} : { file: "1" }) })}`);
  return (
    <Loaded state={tree} what="the file tree">
      {(entries) => (
        <ContainedList label={dir === "" ? "Files at the root" : `Files in ${dir}`} kind="on-page">
          {entries.length === 0 && <ContainedListItem>This directory is empty.</ContainedListItem>}
          {entries.toSorted((a, b) => b.type.localeCompare(a.type) || a.name.localeCompare(b.name))
            .map((entry) => (
              <ContainedListItem key={entry.path} onClick={() => open(entry)}
                renderIcon={entry.type === "tree" ? Folder : Document}
                aria-current={entry.path === selected ? "true" : undefined}>
                {entry.name}{entry.type === "tree" ? "/" : ""}
                {entry.path === selected ? " (open)" : ""}
              </ContainedListItem>
            ))}
        </ContainedList>
      )}
    </Loaded>
  );
}

function FileView({ repo, path, gitRef }: { repo: string; path: string; gitRef: string }) {
  const blob = useApi<Blob>(`/forge/repos/${repo}/blob${query({ ref: gitRef, path })}`);
  return (
    <Loaded state={blob} what={path}>
      {(data) => (
        <CodeSnippet type="multi" feedback="Copied" className="app-full" aria-label={path}
          maxCollapsedNumberOfRows={40}>
          {data.content === "" ? "(empty file)" : data.content}
        </CodeSnippet>
      )}
    </Loaded>
  );
}

const COMMIT_COLUMNS: Column<Commit>[] = [
  { key: "commit", header: "Commit", value: (row) => shortHash(row.commit) },
  { key: "subject", header: "Subject", value: (row) => row.subject },
  { key: "author", header: "Author", value: (row) => row.author },
  { key: "date", header: "Date", value: (row) => row.date,
    render: (row) => formatTime(row.date) },
];

/** One repository: file tree, file viewer and commit log. */
export function RepoPage() {
  const { repo = "" } = useParams();
  const [search] = useSearchParams();
  const gitRef = search.get("ref") ?? "main";
  const path = search.get("path") ?? "";
  const isFile = search.get("file") === "1";
  const dir = isFile ? path.split("/").slice(0, -1).join("/") : path;
  const repos = useApi<Repo[]>("/forge/repos");
  const log = useApi<Commit[]>(`/forge/repos/${repo}/log${query({ ref: gitRef, limit: 50 })}`,
    { live: forgeEvent });
  const info = (repos.data ?? []).find((candidate) => candidate.name === repo);
  return (
    <>
      <header className="app-page-header app-stack-tight">
        <Crumbs repo={repo} path={path} gitRef={gitRef} />
        <h1>{repo}</h1>
        <div className="app-row">
          <Tag type="outline">Ref {gitRef}</Tag>
          {info !== undefined && <span className="app-mono">git clone {cloneUrl(info)}</span>}
        </div>
      </header>
      <div className="app-code-layout">
        <Tree repo={repo} dir={dir} gitRef={gitRef} selected={isFile ? path : ""} />
        {isFile ? <FileView repo={repo} path={path} gitRef={gitRef} />
          : <Tile>Choose a file to read it. Directories open in the list.</Tile>}
      </div>
      <Section title="Commits">
        <Loaded state={log} what="the commit log">
          {(data) => (
            <SimpleTable title={`Commits on ${gitRef}`} rows={data} rowId={(row) => row.commit}
              columns={COMMIT_COLUMNS} empty="This ref has no commit." size="sm" />
          )}
        </Loaded>
      </Section>
    </>
  );
}

/** One pull request with its diff. */
export function PullPage() {
  const { id = "" } = useParams();
  const pull = useApi<{ pull: PullRequest; diff: string }>(`/forge/pulls/${id}`,
    { live: forgeEvent });
  return (
    <Loaded state={pull} what={`pull request ${id}`}>
      {({ pull: pr, diff }) => (
        <div className="app-narrow">
          <header className="app-page-header app-stack-tight">
            <Breadcrumb noTrailingSlash>
              <BreadcrumbItem><RouterLink to="/code">Code</RouterLink></BreadcrumbItem>
              <BreadcrumbItem>
                <RouterLink to={`/code/${pr.repo}`}>{pr.repo}</RouterLink>
              </BreadcrumbItem>
              <BreadcrumbItem isCurrentPage>Pull request #{pr.id}</BreadcrumbItem>
            </Breadcrumb>
            <h1>{pr.title}</h1>
            <div className="app-row">
              <StatusTag status={pr.state} />
              <Tag type="outline">{pr.branch} into {pr.base}</Tag>
              <span className="app-label">
                Opened by {pr.author} on {formatTime(pr.created_at)}
                {pr.merged_at !== "" && ` · merged ${formatTime(pr.merged_at)}`}
              </span>
              {pr.work_item !== "" && (
                <WorkItemLink id={pr.work_item}>Work item {pr.work_item}</WorkItemLink>
              )}
            </div>
          </header>
          {pr.body !== "" && (
            <Section title="Description">
              <Tile className="app-prose app-chat-answer">{pr.body}</Tile>
            </Section>
          )}
          <Section title="Commits">
            <p className="app-mono">base {pr.base_commit}</p>
            <p className="app-mono">head {pr.head_commit}</p>
          </Section>
          <Section title="Diff"><DiffView diff={diff} /></Section>
        </div>
      )}
    </Loaded>
  );
}
