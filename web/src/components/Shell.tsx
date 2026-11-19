import { useCallback, useEffect, useRef, useState } from "react";
import type { ComponentType, ReactNode } from "react";
import { Link as RouterLink, useLocation } from "react-router-dom";
import {
  Content,
  Header,
  HeaderGlobalAction,
  HeaderGlobalBar,
  HeaderMenuButton,
  HeaderName,
  HeaderPanel,
  SideNav,
  SideNavItems,
  SideNavLink,
  SkipToContent,
  Switcher,
  SwitcherDivider,
  SwitcherItem,
} from "@carbon/react";
import {
  Activity,
  Catalog,
  ChartNetwork,
  Chat,
  Code,
  Dashboard,
  EventSchedule,
  Locked,
  Notification,
  PlayOutline,
  Roadmap,
  TaskApproved,
  UserAvatar,
} from "@carbon/icons-react";
import { signOut } from "@/api/client";
import mark from "@/assets/icon.svg";
import { AsOfBanner, AsOfControl } from "@/components/AsOf";
import { ErrorNote } from "@/components/common";
import { IconTag, StatusTag } from "@/components/status";
import { UnverifiedTag } from "@/components/Unverified";
import { useClockError } from "@/state/clock";
import { useLive } from "@/state/live";
import { useSession } from "@/state/session";

interface NavEntry {
  to: string;
  label: string;
  icon: ComponentType;
}

const NAV: NavEntry[] = [
  { to: "/", label: "Overview", icon: Dashboard },
  { to: "/timeline", label: "Timeline", icon: Roadmap },
  { to: "/topology", label: "Topology", icon: ChartNetwork },
  { to: "/upcoming", label: "Upcoming", icon: EventSchedule },
  { to: "/approvals", label: "Approvals", icon: TaskApproved },
  { to: "/activity", label: "Activity", icon: Activity },
  { to: "/code", label: "Code", icon: Code },
  { to: "/access", label: "Access", icon: Locked },
  { to: "/audit", label: "Audit", icon: Catalog },
  { to: "/notifications", label: "Notifications", icon: Notification },
  { to: "/ask", label: "Ask", icon: Chat },
  { to: "/demo", label: "Demo", icon: PlayOutline },
];

function isActive(pathname: string, to: string): boolean {
  if (to === "/") {
    return pathname === "/";
  }
  if (to === "/approvals" && pathname.startsWith("/work/")) {
    return true;
  }
  return pathname === to || pathname.startsWith(`${to}/`);
}

function UserPanel({ expanded, onPick }: { expanded: boolean; onPick: () => void }) {
  const { users, user: signedIn, userName, setUser, signIn } = useSession();
  if (signIn) {
    return (
      <HeaderPanel expanded={expanded} aria-label="Account">
        <p className="app-panel-note">
          Signed in as {signedIn?.email ?? userName} ({signedIn?.role ?? "no role"}).
        </p>
        <Switcher aria-label="Account" expanded={expanded}>
          <SwitcherItem aria-label="Sign out" onClick={() => void signOut()}>
            Sign out
          </SwitcherItem>
        </Switcher>
      </HeaderPanel>
    );
  }
  return (
    <HeaderPanel expanded={expanded} aria-label="Switch user">
      <Switcher aria-label="Users" expanded={expanded}>
        {users.map((user) => (
          <SwitcherItem key={user.name} aria-label={`${user.display}, ${user.role}`}
            isSelected={user.name === userName}
            onClick={() => {
              setUser(user.name);
              onPick();
            }}>
            {user.display} ({user.role}){user.self_declared && <> <UnverifiedTag /></>}
          </SwitcherItem>
        ))}
        <SwitcherDivider />
      </Switcher>
      <p className="app-panel-note">
        There is no sign-in in this prototype. The choice is sent to the server as the
        X-Clanker-User header.
      </p>
    </HeaderPanel>
  );
}

function GlobalBar() {
  const { user, userName, signIn } = useSession();
  const { connected } = useLive();
  const [panel, setPanel] = useState(false);
  return (
    <>
      <HeaderGlobalBar>
        <div className="app-header-status">
          <StatusTag status={connected ? "ok" : "failed"} size="sm"
            label={connected ? "Live" : "Reconnecting"} />
          <span className="app-header-user">
            <IconTag type="outline" size="sm" icon={UserAvatar}>
              {user?.display ?? userName} · {user?.role ?? "unknown role"}
            </IconTag>
            {user?.self_declared === true && <UnverifiedTag />}
          </span>
        </div>
        <AsOfControl />
        <HeaderGlobalAction aria-label={signIn ? "Account" : "Switch user"} isActive={panel} tooltipAlignment="end"
          aria-expanded={panel} onClick={() => setPanel((open) => !open)}>
          <UserAvatar size={20} />
        </HeaderGlobalAction>
      </HeaderGlobalBar>
      <UserPanel expanded={panel} onPick={() => setPanel(false)} />
    </>
  );
}

// Carbon's side navigation is always open from its "lg" breakpoint up. Below it the navigation
// is a drawer over the page, and the menu button of the header opens it.
const WIDE = "(min-width: 66rem)";

/**
 * The open state of the navigation drawer of a narrow window. Focus goes into the navigation
 * when it opens and back to the menu button when it closes. Escape closes it, and so does a
 * window that becomes wide.
 */
function useDrawer() {
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const navigation = useRef<HTMLElement>(null);
  const wasOpen = useRef(false);
  const close = useCallback(() => setOpen(false), []);
  useEffect(() => {
    if (open) {
      navigation.current?.focus();
    } else if (wasOpen.current) {
      button.current?.focus();
    }
    wasOpen.current = open;
  }, [open]);
  useEffect(() => {
    if (!open) {
      return undefined;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        close();
      }
    };
    const wide = window.matchMedia(WIDE);
    window.addEventListener("keydown", onKey);
    wide.addEventListener("change", close);
    return () => {
      window.removeEventListener("keydown", onKey);
      wide.removeEventListener("change", close);
    };
  }, [open, close]);
  return { open, close, toggle: () => setOpen((now) => !now), button, navigation };
}

/** The application frame: header, side navigation, as-of banner and the page. */
export function Shell({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const { usersError } = useSession();
  const clockError = useClockError();
  const { open, close, toggle, button, navigation } = useDrawer();
  return (
    <>
      <Header aria-label="S.R.C. (Site Reliability Clanker)">
        <SkipToContent />
        <HeaderMenuButton ref={button} isActive={open} aria-expanded={open}
          aria-controls="app-navigation"
          aria-label={open ? "Close the navigation" : "Open the navigation"}
          onClick={toggle} />
        <HeaderName as={RouterLink} to="/" prefix="" className="app-header-name">
          <img className="app-mark" src={mark} alt="" width={24} height={24} />
          S.R.C.
          <span className="app-product-name">Site Reliability Clanker</span>
        </HeaderName>
        <GlobalBar />
      </Header>
      <SideNav id="app-navigation" ref={navigation} aria-label="Dashboard areas"
        expanded={open} onOverlayClick={close}>
        <SideNavItems>
          {NAV.map((entry) => (
            <SideNavLink key={entry.to} as={RouterLink} to={entry.to} renderIcon={entry.icon}
              isActive={isActive(pathname, entry.to)} onClick={close}
              {...(isActive(pathname, entry.to) ? { "aria-current": "page" as const } : {})}>
              {entry.label}
            </SideNavLink>
          ))}
        </SideNavItems>
      </SideNav>
      <Content id="main-content" className="app-content" inert={open}>
        <AsOfBanner />
        {usersError !== null && (
          <ErrorNote title="Could not load the users." message={usersError} />
        )}
        {clockError !== null && (
          <ErrorNote title="Could not read the server's clock."
            message={`${clockError} Times relative to "now" use this browser's clock.`} />
        )}
        {children}
      </Content>
    </>
  );
}
