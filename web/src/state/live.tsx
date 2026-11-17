import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState }
  from "react";
import type { ReactNode } from "react";
import type { AuditEvent } from "@/api/types";
import { API_BASE } from "@/api/urls";

/** A live notice is one audit event, or `null` after a reconnect when events were missed. */
export type LiveNotice = AuditEvent | null;
type Listener = (notice: LiveNotice) => void;

interface Live {
  connected: boolean;
  subscribe: (listener: Listener) => () => void;
}

const LiveContext = createContext<Live | null>(null);

function parseEvent(message: MessageEvent<string>): AuditEvent | null {
  try {
    return JSON.parse(message.data) as AuditEvent;
  } catch {
    return null;
  }
}

/** Holds the one server-sent event connection of the dashboard. */
export function LiveProvider({ children }: { children: ReactNode }) {
  const listeners = useRef(new Set<Listener>());
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const source = new EventSource(`${API_BASE}/events/stream`);
    let dropped = false;
    const notify = (notice: LiveNotice) => {
      for (const listener of listeners.current) {
        listener(notice);
      }
    };
    source.addEventListener("open", () => {
      setConnected(true);
      if (dropped) {
        // The server does not replay missed events, so every view must load again.
        notify(null);
      }
      dropped = false;
    });
    source.addEventListener("error", () => {
      dropped = true;
      setConnected(false);
    });
    source.addEventListener("audit", (message) => {
      // An event that cannot be read still means that something changed: every view loads.
      notify(parseEvent(message as MessageEvent<string>));
    });
    return () => source.close();
  }, []);

  // The identity of `subscribe` never changes. A subscriber that subscribed again on every
  // event would drop the reload that it had just scheduled.
  const subscribe = useCallback((listener: Listener) => {
    listeners.current.add(listener);
    return () => {
      listeners.current.delete(listener);
    };
  }, []);
  const value = useMemo<Live>(() => ({ connected, subscribe }), [connected, subscribe]);
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export function useLive(): Live {
  const live = useContext(LiveContext);
  if (live === null) {
    throw new Error("useLive needs a LiveProvider above it");
  }
  return live;
}
