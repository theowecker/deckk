"use strict";

// deckk's link carries the token after #, which never reaches a server. It
// is kept per origin, which includes the port, and cleared from the address.
let token = "";
try {
  const fromLink = new URLSearchParams(location.hash.slice(1)).get("t");
  if (fromLink) localStorage.setItem("deckk-token", fromLink);
  token = fromLink || localStorage.getItem("deckk-token") || "";
} catch {
  token = new URLSearchParams(location.hash.slice(1)).get("t") || "";
}
if (location.hash) history.replaceState(null, "", location.pathname);

const $ = (id) => document.getElementById(id);
const open = new Set();
const histories = new Map();
let state = null;
let raw = "";
let deferred = false;
// Where each open conversation was scrolled, so a redraw keeps the place.
const scrolled = new Map();

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") node.className = value;
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else node.setAttribute(key, value === true ? "" : value);
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false || child === "") continue;
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

function api(path, options = {}) {
  return fetch(path, { ...options, headers: { ...(options.headers || {}), "X-Deckk-Token": token } });
}

async function act(kind, id, done) {
  const response = await api("api/action", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ kind, id }),
  }).catch(() => null);
  if (!response) return toast("deckk isn't answering. Is it still running?");
  toast(response.ok ? done : (await response.text()).trim());
}

let toastTimer;
function toast(text) {
  const node = $("toast");
  node.textContent = text;
  node.classList.add("shown");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => node.classList.remove("shown"), 2800);
}

// A badge's colour comes from the project's name, so it stays the same.
function badge(name) {
  let hash = 0;
  for (const c of name) hash = (hash * 31 + c.codePointAt(0)) >>> 0;
  return el("span", { class: `badge hue-${hash % 6}`, "aria-hidden": "true" }, [...name][0] || "·");
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
  $("clashes").replaceChildren(...clashes.map((c) =>
    el("div", { class: "clash", role: "alert" },
      el("p", {}, `${c.a} and ${c.b} both edited the same files.`),
      el("p", { class: "files" }, c.files.map((f) => f.split("/").pop()).join(", ")),
    )));
}

const kinds = {
  suggestion: "Suggested next prompt",
  ready: "Typed in, ready to send",
  resume: "Pick this back up",
  proposal: "New session",
};

function renderNext(next) {
  const head = [el("h2", { id: "next-title" }, "Next up")];
  if (!next.length) {
    $("next").replaceChildren(...head, el("p", { class: "lede" },
      "Nothing suggested right now. Ask deckk what to do next; press c in deckk."));
    return;
  }
  $("next").replaceChildren(...head,
    el("p", { class: "lede" }, "What deckk suggests."),
    el("div", { class: "card" }, next.map((n) => {
      const kind = n.kind === "suggestion" && n.ready ? "ready" : n.kind;
      const [start, starting] = { proposal: ["Start", "Starting"], resume: ["Resume", "Resuming"] }[n.kind] || ["Open", "Opening"];
      return el("article", { class: "offer" },
        el("div", { class: "offer-head" },
          badge(n.project.split("/").pop()),
          el("div", { class: "who" }, el("strong", {}, n.title), el("span", { class: "kind" }, `${kinds[kind]} · ${n.project}`)),
          el("div", { class: "actions" },
            el("button", { class: "button", "data-focus": `dismiss:${n.id}`, onclick: () => act("dismiss", n.id, "Dismissed") }, "Dismiss"),
            el("button", { class: "button primary", "data-focus": `open:${n.id}`, onclick: () => act("open", n.id, `${starting} in deckk`) }, start),
          ),
        ),
        el("div", { class: "said" },
          el("p", { class: `bubble ${kind === "ready" ? "ready" : "draft"}` }, n.prompt),
          n.why && el("p", { class: "why" }, n.why),
        ),
      );
    })),
  );
}

// The grouping switch is a pair of toggle buttons, as in a macOS toolbar;
// the choice is remembered in this browser.
let grouping = "status";
try {
  if (localStorage.getItem("deckk-grouping") === "project") grouping = "project";
} catch {}

function renderGrouping() {
  const choice = (value, label) => el("button", {
    class: "segment", "aria-pressed": grouping === value ? "true" : "false", "data-focus": `grouping:${value}`,
    onclick: () => {
      grouping = value;
      try { localStorage.setItem("deckk-grouping", value); } catch {}
      render(true);
    },
  }, label);
  $("grouping").replaceChildren(el("div", { class: "segmented", role: "group", "aria-label": "Group sessions" },
    choice("status", "By status"), choice("project", "By project")));
}

const stateLabel = { working: "Working", waiting: "Waiting" };

function statusText(s) {
  if (s.state === "stopped") return s.status || ago(s.since);
  const since = ago(s.since);
  return `${stateLabel[s.state] || s.status}${since === "now" ? "" : ` ${since}`}`;
}

// History loads when a row opens, and again when the session moves on.
async function loadHistory(s) {
  const known = histories.get(s.id);
  if (known && known.since === s.since) return;
  histories.set(s.id, { since: s.since, turns: known ? known.turns : null });
  const response = await api(`api/history?id=${encodeURIComponent(s.id)}`).catch(() => null);
  const turns = response && response.ok ? await response.json() : null;
  histories.set(s.id, { since: s.since, turns });
  render(true);
}

function conversation(s) {
  const loaded = histories.get(s.id);
  const turns = loaded && loaded.turns && loaded.turns.length ? loaded.turns : [{ prompt: s.lastPrompt, reply: s.lastReply }];
  const chat = el("div", { class: "chat", tabindex: "0", role: "log", "data-chat": s.id, "aria-label": `Recent conversation in ${s.title}` },
    turns.flatMap((t) => [
      t.prompt && el("p", { class: "bubble mine" }, t.prompt),
      t.reply && el("p", { class: "bubble them" }, t.reply),
    ]));
  if (!chat.childElementCount) return null;
  const place = scrolled.get(s.id);
  requestAnimationFrame(() => {
    chat.scrollTop = place && !place.atEnd ? place.top : chat.scrollHeight;
  });
  return chat;
}

function safeLink(url) {
  try {
    return new URL(url).protocol === "https:";
  } catch {
    return false;
  }
}

function sessionRow(s) {
  const row = el("li", { class: `row${open.has(s.id) ? " open" : ""}` });
  const toggle = el("button", {
    "data-focus": `row:${s.id}`,
    "aria-expanded": open.has(s.id) ? "true" : "false",
    onclick: () => {
      if (open.has(s.id)) open.delete(s.id);
      else { open.add(s.id); loadHistory(s); }
      render(true);
    },
  },
    el("span", { class: `dot ${s.state}`, "aria-hidden": "true" }),
    el("span", { class: "title" }, s.title,
      s.suggested && el("span", { class: "mark", title: "Has a suggestion", "aria-label": "has a suggestion" }, "✦"),
      s.clash && el("span", { class: "warn", title: s.clash, "aria-label": s.clash }, "⚠"),
    ),
    el("span", { class: `state ${s.state}` }, statusText(s)),
    el("span", { class: "where" }, s.project, s.branch && [" ", el("code", {}, `⎇ ${s.branch}`)], s.where && ` · ${s.where}`),
    s.summary && el("span", { class: `summary${s.stale ? " stale" : ""}` },
      s.stale && el("span", { class: "earlier" }, "Earlier note: "), s.summary),
  );
  row.append(toggle);
  if (open.has(s.id)) {
    const verb = s.state === "stopped" ? "Resume in deckk" : "Open in deckk";
    row.append(el("div", { class: "detail" },
      s.clash && el("p", { class: "note" }, `⚠ ${s.clash}`),
      conversation(s),
      el("div", { class: "facts" },
        el("span", {}, s.folder),
        s.pr && safeLink(s.prUrl) && el("a", { href: s.prUrl, target: "_blank", rel: "noopener noreferrer" }, `PR ${s.pr}`),
        s.model && el("span", {}, s.model),
        s.context && el("span", {}, `${Math.round(s.context / 1000)}k context`),
        s.changed && el("span", {}, plural(s.changed, "uncommitted change")),
      ),
      el("div", { class: "actions" },
        el("button", { class: "button primary", "data-focus": `go:${s.id}`, onclick: () => act("open", s.id, s.state === "stopped" ? "Resuming in deckk" : "Opening in deckk") }, verb),
      ),
    ));
  }
  return row;
}

function renderGroups(groups) {
  if (!groups.length) {
    $("groups").replaceChildren(el("p", { class: "groups-empty" },
      grouping === "project" ? "No project has moved this week." : "No sessions yet. Press n in deckk to start one."));
    return;
  }
  $("groups").replaceChildren(...groups.map((g) =>
    el("section", { "aria-label": g.title },
      el("h3", {}, g.title),
      el("ul", { class: "card" }, g.sessions.map(sessionRow),
        g.hidden > 0 && el("li", { class: "more" }, `${g.hidden} more in deckk`),
      ),
    )));
}

function renderToday(t) {
  const title = el("h2", { id: "today-title" }, "Today");
  if (!t.touched) {
    $("today").replaceChildren(title, el("p", {}, "No sessions have moved yet today."));
    return;
  }
  $("today").replaceChildren(title, el("p", {},
    "You've worked in ", el("strong", {}, plural(t.touched, "session")),
    " across ", el("strong", {}, plural(t.projects, "project")),
    t.busiest ? [", most of it in ", el("strong", {}, t.busiest)] : "", ".",
    t.prs ? [" ", el("strong", {}, plural(t.prs, "pull request")), " this week."] : "",
  ));
}

// A small Markdown subset: headings, lists, paragraphs, bold and code.
function markdown(text) {
  const inline = (line) => {
    const out = [];
    let last = 0;
    for (const m of line.matchAll(/(\*\*[^*]+\*\*|`[^`]+`)/g)) {
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
  for (const rawLine of text.split("\n")) {
    const line = rawLine.trimEnd();
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

// Repeats of the same news fold into one line with a count.
function renderActivity(events) {
  const folded = [];
  for (const e of events) {
    const last = folded[folded.length - 1];
    if (last && last.text === e.text) last.times++;
    else folded.push({ ...e, times: 1 });
  }
  $("activity").replaceChildren(
    el("h2", { id: "activity-title" }, "Activity"),
    folded.length
      ? el("ul", { class: "events" }, folded.slice(0, 8).map((e) =>
          el("li", {},
            el("span", {}, e.text, e.times > 1 && el("span", { class: "times" }, ` ×${e.times}`)),
            el("time", { datetime: e.at }, ago(e.at)))))
      : el("p", {}, "Sessions finishing, opening PRs and clashing show up here."),
  );
}

function selecting() {
  const selection = document.getSelection();
  return Boolean(selection && !selection.isCollapsed && document.querySelector("main").contains(selection.anchorNode));
}

// Re-rendering replaces the nodes, so focus and scrolled conversations are
// put back, and a redraw waits while the user is selecting text.
function render(force = false) {
  if (!state || !state.counts) return;
  if (!force && selecting()) {
    deferred = true;
    return;
  }
  deferred = false;
  const focused = document.activeElement && document.activeElement.dataset.focus;
  for (const chat of document.querySelectorAll("[data-chat]")) {
    scrolled.set(chat.dataset.chat, { top: chat.scrollTop, atEnd: chat.scrollTop + chat.clientHeight >= chat.scrollHeight - 4 });
  }
  renderTallies(state.counts);
  renderClashes(state.clashes || []);
  renderNext(state.nextUp || []);
  renderGrouping();
  renderGroups((grouping === "project" ? state.projects : state.groups) || []);
  renderToday(state.today || {});
  renderGoals(state.goals || "");
  renderActivity(state.activity || []);
  for (const groups of [state.groups || [], state.projects || []]) {
    for (const g of groups) for (const s of g.sessions) if (open.has(s.id)) loadHistory(s);
  }
  if (focused) {
    const again = document.querySelector(`[data-focus="${CSS.escape(focused)}"]`);
    if (again) again.focus({ preventScroll: true });
  }
}

document.addEventListener("selectionchange", () => {
  if (deferred && !selecting()) render();
});

function receive(data) {
  if (data === raw) return;
  raw = data;
  state = JSON.parse(data);
  render();
}

function status(kind, text) {
  const live = $("live");
  live.dataset.state = kind;
  live.textContent = text;
}

// The live stream is read with fetch rather than EventSource, so it can send
// the token as a header like every other request.
async function stream() {
  for (let wait = 1000; ; wait = Math.min(wait * 2, 15000)) {
    const response = await api("api/stream").catch(() => null);
    if (response && response.status === 401) {
      try { localStorage.removeItem("deckk-token"); } catch {}
      status("lost", "Press w in deckk to open this page");
      return;
    }
    if (response && response.ok) {
      status("live", "Live");
      wait = 1000;
      const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
      let buffer = "";
      try {
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += value;
          let end;
          while ((end = buffer.indexOf("\n\n")) >= 0) {
            const event = buffer.slice(0, end);
            buffer = buffer.slice(end + 2);
            const data = event.split("\n").filter((l) => l.startsWith("data: ")).map((l) => l.slice(6)).join("\n");
            if (data) receive(data);
          }
        }
      } catch {}
    }
    status("lost", response ? "Reconnecting" : "deckk isn't running");
    await new Promise((resolve) => setTimeout(resolve, wait));
  }
}

if (!token) status("lost", "Press w in deckk to open this page");
else stream();
setInterval(() => render(), 30000);
