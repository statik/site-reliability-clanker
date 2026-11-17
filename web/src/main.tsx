import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "@/App";
import { ClockProvider } from "@/state/clock";
import { LiveProvider } from "@/state/live";
import { SessionProvider } from "@/state/session";
import "@carbon/charts-react/styles.css";
import "@/styles/index.scss";
import "@/styles/timeline.scss";
import "@/styles/topology.scss";

const root = document.getElementById("root");
if (root === null) {
  throw new Error("index.html has no #root element");
}

createRoot(root).render(
  <StrictMode>
    <BrowserRouter>
      <SessionProvider>
        <LiveProvider>
          <ClockProvider>
            <App />
          </ClockProvider>
        </LiveProvider>
      </SessionProvider>
    </BrowserRouter>
  </StrictMode>,
);
