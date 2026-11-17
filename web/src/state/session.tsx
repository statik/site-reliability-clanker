import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { apiGet, errorMessage, storedUser, storeUser } from "@/api/client";
import type { User } from "@/api/types";

interface Session {
  users: User[];
  user: User | null;
  userName: string;
  usersError: string | null;
  setUser: (name: string) => void;
  /** True when people sign in. False in the demo, where the user switcher names the user. */
  signIn: boolean;
  asOf: Date | null;
  setAsOf: (time: Date | null) => void;
}

const SessionContext = createContext<Session | null>(null);

/** Holds who is "signed in", and the global point-in-time control. */
export function SessionProvider({ children }: { children: ReactNode }) {
  const [users, setUsers] = useState<User[]>([]);
  const [usersError, setUsersError] = useState<string | null>(null);
  const [userName, setUserName] = useState(storedUser);
  const [asOf, setAsOf] = useState<Date | null>(null);
  const [signIn, setSignIn] = useState(false);

  useEffect(() => {
    // With a sign-in the server knows who this is. The stored choice of the demo is not used.
    apiGet<{ sign_in: boolean }>("/auth")
      .then((auth) => {
        setSignIn(auth.sign_in);
        return auth.sign_in ? apiGet<User>("/me") : null;
      })
      .then(
        (me) => {
          if (me !== null) {
            setUserName(me.name);
          }
        },
        (error: unknown) => setUsersError(errorMessage(error)),
      );
  }, []);

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
      signIn,
      asOf,
      setAsOf,
    }),
    [users, userName, usersError, setUser, signIn, asOf],
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
