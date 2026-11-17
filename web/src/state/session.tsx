import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { apiGet, errorMessage, storedUser, storeUser } from "@/api/client";
import type { User } from "@/api/types";

export type ThemeName = "g10" | "g100";
const THEME_KEY = "clanker.theme";

interface Session {
  users: User[];
  user: User | null;
  userName: string;
  usersError: string | null;
  setUser: (name: string) => void;
  theme: ThemeName;
  toggleTheme: () => void;
  asOf: Date | null;
  setAsOf: (time: Date | null) => void;
}

const SessionContext = createContext<Session | null>(null);

function systemTheme(): ThemeName {
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "g100" : "g10";
}

function storedTheme(): ThemeName | null {
  const value = localStorage.getItem(THEME_KEY);
  return value === "g10" || value === "g100" ? value : null;
}

/** Follows the system colour scheme until the person picks a theme with the header toggle. */
function useTheme(): [ThemeName, () => void] {
  const [chosen, setChosen] = useState<ThemeName | null>(storedTheme);
  const [system, setSystem] = useState<ThemeName>(systemTheme);
  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setSystem(systemTheme());
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);
  const theme = chosen ?? system;
  useEffect(() => {
    document.documentElement.dataset["carbonTheme"] = theme;
  }, [theme]);
  const toggle = useCallback(() => {
    const next: ThemeName = theme === "g100" ? "g10" : "g100";
    localStorage.setItem(THEME_KEY, next);
    setChosen(next);
  }, [theme]);
  return [theme, toggle];
}

/** Holds who is "signed in", the theme, and the global point-in-time control. */
export function SessionProvider({ children }: { children: ReactNode }) {
  const [users, setUsers] = useState<User[]>([]);
  const [usersError, setUsersError] = useState<string | null>(null);
  const [userName, setUserName] = useState(storedUser);
  const [theme, toggleTheme] = useTheme();
  const [asOf, setAsOf] = useState<Date | null>(null);

  useEffect(() => {
    apiGet<User[]>("/users").then(
      (list) => {
        setUsers(list);
        setUsersError(null);
      },
      (error: unknown) => setUsersError(errorMessage(error)),
    );
  }, []);

  const setUser = useCallback((name: string) => {
    storeUser(name);
    setUserName(name);
  }, []);

  const value = useMemo<Session>(
    () => ({
      users,
      user: users.find((candidate) => candidate.name === userName) ?? null,
      userName,
      usersError,
      setUser,
      theme,
      toggleTheme,
      asOf,
      setAsOf,
    }),
    [users, userName, usersError, setUser, theme, toggleTheme, asOf],
  );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): Session {
  const session = useContext(SessionContext);
  if (session === null) {
    throw new Error("useSession needs a SessionProvider above it");
  }
  return session;
}
