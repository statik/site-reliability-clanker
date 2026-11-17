import { Route, Routes } from "react-router-dom";
import { AppLink, PageHeader } from "@/components/common";
import { Shell } from "@/components/Shell";
import { AccessPage } from "@/pages/AccessPage";
import { AskPage, DemoPage } from "@/pages/AskDemoPages";
import { AuditPage } from "@/pages/AuditPage";
import { CodePage, PullPage, RepoPage } from "@/pages/CodePages";
import { ActivityPage, ApprovalsPage } from "@/pages/ListPages";
import { NotificationsPage } from "@/pages/NotificationsPage";
import { OverviewPage } from "@/pages/OverviewPage";
import { TimelinePage } from "@/pages/TimelinePage";
import { TopologyPage } from "@/pages/TopologyPage";
import { UpcomingPage } from "@/pages/UpcomingPage";
import { WorkItemPage } from "@/pages/workitem/WorkItemPage";

function NotFound() {
  return (
    <>
      <PageHeader title="Page not found">This address is not a page of the dashboard.</PageHeader>
      <AppLink to="/">Go to the overview</AppLink>
    </>
  );
}

export function App() {
  return (
    <Shell>
      <Routes>
        <Route path="/" element={<OverviewPage />} />
        <Route path="/timeline" element={<TimelinePage />} />
        <Route path="/topology" element={<TopologyPage />} />
        <Route path="/upcoming" element={<UpcomingPage />} />
        <Route path="/approvals" element={<ApprovalsPage />} />
        <Route path="/work/:id" element={<WorkItemPage />} />
        <Route path="/activity" element={<ActivityPage />} />
        <Route path="/code" element={<CodePage />} />
        <Route path="/code/pulls/:id" element={<PullPage />} />
        <Route path="/code/:repo" element={<RepoPage />} />
        <Route path="/access" element={<AccessPage />} />
        <Route path="/audit" element={<AuditPage />} />
        <Route path="/notifications" element={<NotificationsPage />} />
        <Route path="/ask" element={<AskPage />} />
        <Route path="/demo" element={<DemoPage />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </Shell>
  );
}
