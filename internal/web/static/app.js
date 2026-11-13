"use strict";

// deckk's link sets a cookie; changes also need the token as a header, which
// the page asks for with that cookie.
let token = "";

const $ = (id) => document.getElementById(id);
const open = new Set();
let state = null;
let raw = "";

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") node.className = value;
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else node.setAttribute(key, value === true ? "" : value);
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

function ago(when) {
  const seconds = Math.max(0, (Date.now() - new Date(when)) / 1000);
  if (seconds < 60) return "now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d`;
  return new Date(when).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function plural(n, word) {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

async function act(kind, id, done) {
  const response = await fetch("api/action", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Deckk-Token": token },
    body: JSON.stringify({ kind, id }),
  }).catch(() => null);
  toast(response && response.ok ? done : "deckk didn't answer. Is the dashboard still running?");
}

let toastTimer;
function toast(text) {
  const node = $("toast");
  node.textContent = text;
  node.classList.add("shown");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => node.classList.remove("shown"), 2600);
}

function renderTallies(c) {
  const parts = [];
  if (c.waiting) parts.push([c.waiting, "waiting on you"]);
  if (c.working) parts.push([c.working, "working"]);
  if (c.resumable) parts.push([c.resumable, "resumable"]);
  $("tallies").replaceChildren(
    ...parts.flatMap(([n, label], i) => [i ? ", " : "", el("strong", {}, n), " ", label]),
  );
}

function renderClashes(clashes) {
  $("clashes").replaceChildren(
    ...clashes.map((c) =>
      el("div", { class: "clash", role: "alert" },
        el("div", {},
          el("p", {}, `${c.a} and ${c.b} both edited the same files.`),
          el("p", { class: "files" }, c.files.map((f) => f.split("/").pop()).join(", ")),
        ),
      ),
    ),
  );
}

const kinds = {
  suggestion: "Suggested next prompt",
  ready: "Typed in, ready to send",
  resume: "Pick this back up",
  proposal: "New session",
};

function renderNext(next) {
  const section = $("next");
  const title = el("h2", { id: "next-title" }, "Next up");
  if (!next.length) {
    section.replaceChildren(title, el("p", { class: "empty" },
      "Nothing suggested right now. Ask the deckk session what to do next, or press c on the dashboard."));
    return;
  }
  section.replaceChildren(title,
    el("p", { class: "lede" }, "What the deckk session thinks you'd say next. Opening one puts the prompt in place for you to send."),
    ...next.map((n) => {
      const kind = n.kind === "suggestion" && n.ready ? "ready" : n.kind;
      const [start, starting] = { proposal: ["Start", "Starting"], resume: ["Resume", "Resuming"] }[n.kind] || ["Open", "Opening"];
      return el("article", { class: "offer" },
        el("div", { class: "who" },
          el("strong", {}, n.title), el("span", {}, n.project),
          el("div", { class: "kind" }, kinds[kind]),
        ),
        el("div", { class: "actions" },
          el("button", { class: "button", "data-focus": `dismiss:${n.id}`, onclick: () => act("dismiss", n.id, "Dismissed") }, "Dismiss"),
          el("button", { class: "button primary", "data-focus": `open:${n.id}`, onclick: () => act("open", n.id, `${starting} in deckk`) }, start),
        ),
        el("div", { class: "said" },
          el("p", { class: `bubble ${kind === "ready" ? "ready" : "draft"}` }, n.prompt),
          n.why && el("p", { class: "why" }, n.why),
        ),
      );
    }),
  );
}

const stateLabel = { working: "Working", waiting: "Waiting", stopped: "", other: "" };

function sessionRow(s) {
  const key = s.id;
  const row = el("li", { class: `row${open.has(key) ? " open" : ""}` });
  const status = s.state === "stopped" ? (s.status || ago(s.since)) : `${stateLabel[s.state] || s.status} ${ago(s.since) === "now" ? "" : ago(s.since)}`.trim();
  const toggle = el("button", {
    "data-focus": `row:${key}`,
    "aria-expanded": open.has(key) ? "true" : "false",
    onclick: () => {
      open.has(key) ? open.delete(key) : open.add(key);
      row.classList.toggle("open");
      toggle.setAttribute("aria-expanded", open.has(key) ? "true" : "false");
    },
  },
    el("span", { class: `dot ${s.state}`, "aria-hidden": "true" }),
    el("span", { class: "title" }, s.title,
      s.suggested && el("span", { class: "mark", title: "Has a suggestion", "aria-label": "has a suggestion" }, "✦"),
      s.clash && el("span", { class: "warn", title: s.clash, "aria-label": s.clash }, "⚠"),
    ),
    el("span", { class: `state ${s.state}` }, status),
    el("span", { class: "where" }, s.project, s.branch && [" ", el("code", {}, `⎇ ${s.branch}`)], s.where && ` · ${s.where}`),
    s.summary && el("span", { class: `summary${s.stale ? " stale" : ""}`, title: s.stale ? "Written before the session's last turn" : null }, s.summary),
  );
  const chat = el("div", { class: "chat" },
    s.lastPrompt && el("p", { class: "bubble mine" }, s.lastPrompt),
    s.lastReply && el("p", { class: "bubble them" }, s.lastReply),
  );
  const facts = el("div", { class: "facts" },
    el("span", {}, s.folder),
    s.pr && safeLink(s.prUrl) && el("a", { href: s.prUrl, target: "_blank", rel: "noopener noreferrer" }, `PR ${s.pr}`),
    s.model && el("span", {}, s.model),
    s.context && el("span", {}, `${Math.round(s.context / 1000)}k context`),
    s.changed && el("span", {}, plural(s.changed, "uncommitted change")),
  );
  const verb = s.state === "stopped" ? "Resume in deckk" : "Open in deckk";
  row.append(toggle, el("div", { class: "detail" },
    s.clash && el("p", { class: "note" }, `⚠ ${s.clash}`),
    chat, facts,
    el("div", { class: "actions" },
      el("button", { class: "button primary", "data-focus": `go:${s.id}`, onclick: () => act("open", s.id, s.state === "stopped" ? "Resuming in deckk" : "Opening in deckk") }, verb),
    ),
  ));
  return row;
}

function renderGroups(groups) {
  $("groups").replaceChildren(...groups.map((g) =>
    el("section", { "aria-label": g.title },
      el("h3", {}, g.title),
      el("ul", { class: "list" }, g.sessions.map(sessionRow),
        g.hidden > 0 && el("li", { class: "more" }, `${g.hidden} more in the terminal`),
      ),
    ),
  ));
}

function renderToday(t) {
  const lines = [el("h2", {}, "Today")];
  if (!t.touched) {
    lines.push(el("p", {}, "No sessions have moved yet today."));
  } else {
    lines.push(el("p", {},
      "You've worked in ", el("strong", {}, plural(t.touched, "session")),
      " across ", el("strong", {}, plural(t.projects, "project")),
      t.busiest ? [", most of it in ", el("strong", {}, t.busiest)] : "", ".",
      t.prs ? [" ", el("strong", {}, plural(t.prs, "pull request")), " this week."] : "",
    ));
  }
  $("today").replaceChildren(...lines);
}

// A small Markdown subset: headings, lists, paragraphs, bold and code.
function markdown(text) {
  const inline = (line) => {
    const out = [];
    const re = /(\*\*[^*]+\*\*|`[^`]+`)/g;
    let last = 0;
    for (const m of line.matchAll(re)) {
      out.push(line.slice(last, m.index));
      const part = m[0];
      out.push(part.startsWith("**") ? el("strong", {}, part.slice(2, -2)) : el("code", {}, part.slice(1, -1)));
      last = m.index + part.length;
    }
    out.push(line.slice(last));
    return out;
  };
  const nodes = [];
  let list = null;
  for (const raw of text.split("\n")) {
    const line = raw.trimEnd();
    const heading = line.match(/^(#{1,3})\s+(.*)/);
    const item = line.match(/^\s*[-*]\s+(?:\[[ x]\]\s+)?(.*)/);
    if (item) {
      if (!list) nodes.push((list = el("ul")));
      list.append(el("li", {}, inline(item[1])));
      continue;
    }
    list = null;
    if (heading) nodes.push(el(`h${heading[1].length}`, {}, inline(heading[2])));
    else if (line) nodes.push(el("p", {}, inline(line)));
  }
  return nodes;
}

function renderGoals(goals) {
  $("goals").replaceChildren(
    el("h2", { id: "goals-title" }, "Goals"),
    goals.trim()
      ? el("div", { class: "goals" }, markdown(goals))
      : el("p", {}, "No goals yet. Tell the deckk session what you're working toward and it will keep them here."),
  );
}

function renderActivity(events) {
  $("activity").replaceChildren(
    el("h2", { id: "activity-title" }, "Activity"),
    events.length
      ? el("ul", { class: "events" }, events.map((e) =>
          el("li", {}, el("time", { datetime: e.at }, new Date(e.at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })), el("span", {}, e.text))))
      : el("p", {}, "Sessions finishing, opening PRs and clashing show up here."),
  );
}

function safeLink(url) {
  try {
    return new URL(url).protocol === "https:";
  } catch {
    return false;
  }
}

// Re-rendering replaces the nodes, so focus is put back by name.
function render() {
  if (!state || !state.counts) return;
  const focused = document.activeElement && document.activeElement.dataset.focus;
  renderTallies(state.counts);
  renderClashes(state.clashes || []);
  renderNext(state.nextUp || []);
  renderGroups(state.groups || []);
  renderToday(state.today || {});
  renderGoals(state.goals || "");
  renderActivity(state.activity || []);
  if (focused) {
    const again = document.querySelector(`[data-focus="${CSS.escape(focused)}"]`);
    if (again) again.focus({ preventScroll: true });
  }
}

function receive(data) {
  if (data === raw) return;
  raw = data;
  state = JSON.parse(data);
  render();
}

function lost(text) {
  const live = $("live");
  live.dataset.state = "lost";
  live.textContent = text;
}

async function connect() {
  const live = $("live");
  const reply = await fetch("api/token").catch(() => null);
  if (!reply || !reply.ok) {
    lost(reply ? "Press w in deckk to open this page" : "deckk isn't running");
    return;
  }
  token = (await reply.json()).token;
  fetch("api/state").then((r) => r.text()).then((data) => { if (!raw) receive(data); }).catch(() => {});
  const stream = new EventSource("api/stream");
  stream.onopen = () => { live.dataset.state = "live"; live.textContent = "Live"; };
  stream.onmessage = (event) => receive(event.data);
  stream.onerror = () => {
    if (stream.readyState === EventSource.CLOSED) lost("Press w in deckk to open this page");
    else lost("Reconnecting");
  };
}

setInterval(render, 30000);
connect();
