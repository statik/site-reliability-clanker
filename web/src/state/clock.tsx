import { createContext, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { apiGet, errorMessage } from "@/api/client";
import type { SimState } from "@/api/types";
import { useLive } from "@/state/live";

const TICK_MS = 15_000;
const POLL_MS = 60_000;
const EVENT_DELAY_MS = 1000;

interface Clock {
  now: Date;
  /** Why the server's clock could not be read. The browser's clock stands in. */
  error: string | null;
}

const ClockContext = createContext<Clock | null>(null);

/**
 * Tracks the virtual clock of the server. Every "now" on the dashboard comes from here, never
 * from the browser, because a simulator scene can advance the server by days. The offset is
 * measured from the simulator state, on a timer and after each burst of live events.
 */
export function ClockProvider({ children }: { children: ReactNode }) {
  const { subscribe } = useLive();
  const [offsetMs, setOffsetMs] = useState(0);
  const [tick, setTick] = useState(() => Date.now());
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const measure = (serverNow: string) => {
      setOffsetMs(new Date(serverNow).getTime() - Date.now());
      setTick(Date.now());
    };
    const load = () => {
      apiGet<SimState>("/sim").then(
        (sim) => {
          measure(sim.now);
          setError(null);
        },
        (failure: unknown) => setError(errorMessage(failure)),
      );
    };
    load();
    const poll = window.setInterval(load, POLL_MS);
    const ticker = window.setInterval(() => setTick(Date.now()), TICK_MS);
    // An event can mean that the simulator moved the clock. An event's own time is not used,
    // because a writer may set it to a time other than now.
    let pending: number | undefined;
    const unsubscribe = subscribe(() => {
      pending ??= window.setTimeout(() => {
        pending = undefined;
        load();
      }, EVENT_DELAY_MS);
    });
    return () => {
      window.clearInterval(poll);
      window.clearInterval(ticker);
      window.clearTimeout(pending);
      unsubscribe();
    };
  }, [subscribe]);

  const clock = useMemo<Clock>(() => ({ now: new Date(tick + offsetMs), error }),
    [tick, offsetMs, error]);
  return <ClockContext.Provider value={clock}>{children}</ClockContext.Provider>;
}

function useClock(): Clock {
  const clock = useContext(ClockContext);
  if (clock === null) {
    throw new Error("the clock needs a ClockProvider above it");
  }
  return clock;
}

export function useServerNow(): Date {
  return useClock().now;
}

export function useClockError(): string | null {
  return useClock().error;
}
