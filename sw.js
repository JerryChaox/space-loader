// Routes every request made by the Space frame (and the frame's own
// navigation under ./s/) to the loader page, which fetches it through
// the tailcat tunnel.
const SCOPE = new URL(self.registration.scope);
const PREFIX = SCOPE.pathname + "s/";

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

async function isSpaceClient(id) {
  if (!id) return false;
  const c = await self.clients.get(id);
  return !!c && new URL(c.url).pathname.startsWith(PREFIX);
}

function tunnelPath(url) {
  const p = url.pathname.startsWith(PREFIX) ? "/" + url.pathname.slice(PREFIX.length) : url.pathname;
  return p + url.search;
}

async function loaderClient() {
  const all = await self.clients.matchAll({ type: "window" });
  return all.find((c) => !new URL(c.url).pathname.startsWith(PREFIX));
}

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== SCOPE.origin) return;
  event.respondWith((async () => {
    const inSpace = url.pathname.startsWith(PREFIX) || (await isSpaceClient(event.clientId));
    if (!inSpace) return fetch(event.request);
    const loader = await loaderClient();
    if (!loader) return new Response("loader page not open", { status: 502 });
    const req = event.request;
    const body = ["GET", "HEAD"].includes(req.method) ? null : new Uint8Array(await req.arrayBuffer());
    const headers = [];
    req.headers.forEach((v, k) => {
      if (k !== "accept-encoding") headers.push([k, v]);
    });
    const ch = new MessageChannel();
    const reply = new Promise((resolve) => (ch.port1.onmessage = (m) => resolve(m.data)));
    loader.postMessage({ type: "space-fetch", method: req.method, path: tunnelPath(url), headers, body }, [ch.port2]);
    const r = await reply;
    if (r.error) return new Response("tunnel error: " + r.error, { status: 502 });
    const nullBody = [101, 204, 205, 304].includes(r.status) || req.method === "HEAD";
    const h = new Headers();
    for (const [k, v] of r.headers) {
      if (!["content-encoding", "transfer-encoding", "connection"].includes(k.toLowerCase())) h.append(k, v);
    }
    if (r.status >= 300 && r.status < 400 && h.has("location")) {
      // Keep redirects inside the frame's path prefix.
      const loc = new URL(h.get("location"), "http://space/");
      if (loc.host === "space") h.set("location", PREFIX + loc.pathname.slice(1) + loc.search);
    }
    return new Response(nullBody ? null : r.body, { status: r.status, statusText: r.statusText, headers: h });
  })());
});
