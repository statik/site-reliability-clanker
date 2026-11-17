// A minimal static file server for the site directory, used by the check, the capture
// and the video recorder so that none of them needs a separate server process.

import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const SITE_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const DEFAULT_PORT = 4173;

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".mp4": "video/mp4",
  ".srt": "text/plain; charset=utf-8",
};

export function serveSite(port = DEFAULT_PORT) {
  const server = createServer(async (request, response) => {
    const path = decodeURIComponent(new URL(request.url, "http://localhost").pathname);
    const relative = normalize(path === "/" ? "/index.html" : path);
    const file = join(SITE_DIR, relative);
    if (!file.startsWith(SITE_DIR)) {
      response.writeHead(403).end("forbidden");
      return;
    }
    try {
      const body = await readFile(file);
      response.writeHead(200, {
        "Content-Type": TYPES[extname(file)] ?? "application/octet-stream",
        "Cache-Control": "no-store",
      });
      response.end(body);
    } catch (error) {
      response.writeHead(error.code === "ENOENT" ? 404 : 500).end(`${error.code ?? "error"}`);
    }
  });
  return new Promise((resolveServer, reject) => {
    server.once("error", (error) => {
      reject(new Error(`cannot serve the site on port ${port}: ${error.message}`));
    });
    server.listen(port, "127.0.0.1", () => {
      resolveServer({
        url: `http://127.0.0.1:${port}`,
        close: () => new Promise((done) => server.close(done)),
      });
    });
  });
}
