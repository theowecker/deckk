"use strict";

// deckk's link carries the token after #, which never reaches a server. It
// is kept per origin, which includes the port, and cleared from the address.
let token = "";
{
  const fromLink = new URLSearchParams(location.hash.slice(1)).get("t");
  try {
    if (fromLink) localStorage.setItem("deckk-token", fromLink);
    token = fromLink || localStorage.getItem("deckk-token") || "";
  } catch {
    token = fromLink || "";
  }
  if (fromLink) history.replaceState(null, "", location.pathname);
}

const $ = (id) => document.getElementById(id);
const sections = ["home", "sessions", "deckk"];
const open = new Set();
const histories = new Map();
const drafts = new Map();
const scrolled = new Map();
let state = null;
let raw = "";
let deferred = false;
let noticeSeen = 0;
let found = null;
let query = "";
let renaming = "";
let editingGoals = false;
let grouping = "status";
try {
  const saved = localStorage.getItem("deckk-grouping");
  if (["status", "project", "background"].includes(saved)) grouping = saved;
} catch {}

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") node.className = value;
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "value") node.value = value;
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

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

function api(path, options = {}) {
  return fetch(path, { ...options, headers: { ...(options.headers || {}), "X-Deckk-Token": token } });
}

// Results of actions come back as the dashboard's notices; this only
// covers a page that can't reach deckk at all.
async function act(kind, fields = {}, done = "") {
  const response = await api("api/action", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ kind, ...fields }),
  }).catch(() => null);
  if (!response) return toast("deckk isn't answering. Is it still running?", true);
  if (!response.ok) return toast((await response.text()).trim(), true);
  if (done) toast(done);
}

let toastTimer;
function toast(text, error = false) {
  const node = $("toast");
  node.textContent = text;
  node.classList.toggle("error", error);
  node.classList.add("shown");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => node.classList.remove("shown"), 3200);
}

function badge(name) {
  let hash = 0;
  for (const c of name) hash = (hash * 31 + c.codePointAt(0)) >>> 0;
  return el("span", { class: `badge hue-${hash % 6}`, "aria-hidden": "true" }, [...name][0] || "·");
}

function safeLink(url) {
  try {
    return new URL(url).protocol === "https:";
  } catch {
    return false;
  }
}

/* Moving between sections. */

function current() {
  const name = location.hash.slice(1);
  return sections.includes(name) ? name : "home";
}

function showSection(animate) {
  const name = current();
  // Pages are named page-home and so on, not home, so the address never
  // makes the browser scroll to them.
  if (animate) window.scrollTo({ top: 0 });
  for (const s of sections) {
    const page = $(`page-${s}`);
    page.hidden = s !== name;
    if (s === name && animate) {
      page.classList.remove("entering");
      void page.offsetWidth;
      page.classList.add("entering");
    }
  }
  for (const link of document.querySelectorAll(".nav a")) {
    if (link.dataset.section === name) link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
  }
  if (name === "deckk") loadChat(true);
}

window.addEventListener("hashchange", () => showSection(true));

function go(section, sessionID) {
  if (sessionID) {
    open.add(sessionID);
    query = "";
    $("search").value = "";
    found = null;
  }
  if (current() !== section) location.hash = section;
  render(true);
  if (sessionID) {
    requestAnimationFrame(() => {
      const row = document.querySelector(`[data-row="${CSS.escape(sessionID)}"]`);
      if (row) row.scrollIntoView({ behavior: "smooth", block: "center" });
    });
  }
}

/* Home: what's next and what's happening. */

function renderSidebar(s) {
  const waiting = s.counts.waiting;
  const home = $("count-home");
  home.textContent = waiting || "";
  home.classList.toggle("alert", waiting > 0);
  $("count-sessions").textContent = s.counts.waiting + s.counts.working || "";
  $("count-deckk").textContent = (s.nextUp || []).length || "";
  document.body.toggleAttribute("data-working", s.counts.working > 0 || (s.deckk && s.deckk.working));
}

function renderHomeHead(s) {
  const now = new Date();
  const hour = now.getHours();
  const greeting = hour < 12 ? "Good morning" : hour < 18 ? "Good afternoon" : "Good evening";
  $("home-title").textContent = greeting;
  const parts = [now.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" })];
  if (s.counts.waiting) parts.push(`${s.counts.waiting} waiting on you`);
  if (s.counts.working) parts.push(`${s.counts.working} working`);
  $("home-subtitle").textContent = parts.join(" · ");
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
  if (!next.length) {
    $("next").replaceChildren(el("p", { class: "empty-card" },
      "Nothing suggested right now. Ask deckk what to do next in the deckk section."));
    return;
  }
  $("next").replaceChildren(el("div", { class: "card" }, next.map((n) => {
    const kind = n.kind === "suggestion" && n.ready ? "ready" : n.kind;
    const [start, starting] = { proposal: ["Start", "Starting"], resume: ["Resume", "Resuming"] }[n.kind] || ["Open", "Opening"];
    return el("article", { class: "offer" },
      el("div", { class: "offer-head" },
        badge(n.project.split("/").pop()),
        el("div", { class: "who" }, el("strong", {}, n.title), el("span", { class: "kind" }, `${kinds[kind]} · ${n.project}`)),
        el("div", { class: "actions" },
          el("button", { class: "button", "data-focus": `dismiss:${n.id}`, onclick: () => act("dismiss", { id: n.id }) }, "Dismiss"),
          el("button", { class: "button primary", "data-focus": `open:${n.id}`, onclick: () => act("open", { id: n.id }, `${starting} in deckk`) }, start),
        ),
      ),
      el("div", { class: "said" },
        el("p", { class: `bubble ${kind === "ready" ? "ready" : "draft"}` }, n.prompt),
        n.why && el("p", { class: "why" }, n.why),
      ),
    );
  })));
}

function liveSessions(s) {
  const all = (s.groups || []).flatMap((g) => g.sessions);
  const order = { waiting: 0, working: 1 };
  return all.filter((x) => x.state === "waiting" || x.state === "working")
    .sort((a, b) => order[a.state] - order[b.state] || new Date(a.since) - new Date(b.since));
}

function renderNow(s) {
  const live = liveSessions(s);
  if (!live.length) {
    $("now").replaceChildren(el("p", { class: "empty-card" }, "Nothing is running. Start a session with n in deckk, or resume one from Sessions."));
    return;
  }
  $("now").replaceChildren(el("div", { class: "now" }, live.map((x) =>
    el("button", { class: "tile", "data-focus": `tile:${x.id}`, onclick: () => go("sessions", x.id) },
      el("span", { class: `tile-state ${x.state}` }, el("span", { class: "pulse", "aria-hidden": "true" }),
        x.state === "waiting" ? `Waiting ${ago(x.since) === "now" ? "" : ago(x.since)}` : "Working"),
      el("strong", {}, x.title),
      el("span", { class: "tile-note" }, x.suggestion ? `✦ ${x.suggestion}` : x.summary || x.project),
    ))));
}

function renderToday(t) {
  const title = el("h2", {}, "Today");
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

function renderActivity(events) {
  const folded = [];
  for (const e of events) {
    const last = folded[folded.length - 1];
    if (last && last.text === e.text) last.times++;
    else folded.push({ ...e, times: 1 });
  }
  $("activity").replaceChildren(
    el("h2", {}, "Activity"),
    folded.length
      ? el("ul", { class: "events" }, folded.slice(0, 8).map((e) =>
          el("li", {},
            el("span", {}, e.text, e.times > 1 && el("span", { class: "times" }, ` ×${e.times}`)),
            el("time", { datetime: e.at }, ago(e.at)))))
      : el("p", {}, "Sessions finishing, opening PRs and clashing show up here."),
  );
}

/* Sessions: finding, opening and keeping them. */

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
    choice("status", "By status"), choice("project", "By project"), choice("background", "Background")));
}

const stateLabel = { working: "Working", waiting: "Waiting" };

function statusText(s) {
  if (s.state === "stopped") return s.status || ago(s.since);
  const since = ago(s.since);
  return `${stateLabel[s.state] || s.status || ""}${since === "now" ? "" : ` ${since}`}`.trim();
}

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
    ]),
    s.state === "working" && thinking("Claude is working"));
  if (!chat.childElementCount) return null;
  keepScroll(chat, s.id);
  return chat;
}

// thinking is the bubble that stands in for a reply on its way.
function thinking(label) {
  return el("p", { class: "bubble them thinking", "aria-label": label }, el("span", { class: "typing", "aria-hidden": "true" }, el("i"), el("i"), el("i")));
}

function keepScroll(chat, key) {
  const place = scrolled.get(key);
  requestAnimationFrame(() => {
    chat.style.scrollBehavior = "auto";
    chat.scrollTop = place && !place.atEnd ? place.top : chat.scrollHeight;
    chat.style.scrollBehavior = "";
  });
}

function composer(key, placeholder, onSubmit, hint) {
  const area = el("textarea", {
    rows: "1", placeholder, "data-focus": `compose:${key}`, "aria-label": placeholder,
    value: drafts.get(key) || "",
    oninput: (e) => drafts.set(key, e.target.value),
    onkeydown: (e) => {
      // Enter sends and Shift+Enter starts a new line, as in Claude Code.
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        submit(true);
      }
    },
  });
  const submit = (send) => {
    const text = area.value.trim();
    if (!text) return;
    drafts.delete(key);
    area.value = "";
    onSubmit(text, send);
  };
  return el("div", { class: "composer" },
    area,
    el("div", { class: "actions" }, hint.typeIn && el("button", { class: "button", onclick: () => submit(false) }, "Type in"),
      el("button", { class: "button primary", onclick: () => submit(true) }, "Send")),
    el("p", { class: "composer-hint" }, hint.text),
  );
}

function detailBody(s) {
  const verb = s.state === "stopped" ? "Resume" : "Open";
  const body = el("div", { class: "detail-body" },
    s.clash && el("p", { class: "note" }, `⚠ ${s.clash}`),
    s.suggestion && el("div", { class: "suggested" },
      el("span", { class: "label" }, s.ready ? "Typed in, ready to send" : "deckk suggests"),
      el("div", { class: "actions" },
        el("button", { class: "button", onclick: () => act("dismiss", { id: s.id }) }, "Dismiss"),
        el("button", { class: "button primary", onclick: () => act("open", { id: s.id }, `${verb === "Open" ? "Opening" : "Resuming"} in deckk`) }, verb)),
      el("p", { class: `bubble ${s.ready ? "ready" : "draft"}` }, s.suggestion),
      s.why && el("p", { class: "why" }, s.why),
    ),
    conversation(s),
    composer(s.id, s.running ? "Write to this session" : "Resume and write to this session",
      (text, send) => act("prompt", { id: s.id, text, send }),
      { typeIn: true, text: "Enter sends, Shift+Enter adds a line. Type in leaves it in the session's prompt for you to check in deckk." }),
    el("div", { class: "facts" },
      el("span", {}, s.folder),
      s.pr && safeLink(s.prUrl) && el("a", { href: s.prUrl, target: "_blank", rel: "noopener noreferrer" }, `PR ${s.pr}`),
      s.model && el("span", {}, s.model),
      s.context && el("span", {}, `${Math.round(s.context / 1000)}k context`),
      s.changed && el("span", {}, plural(s.changed, "uncommitted change")),
    ),
    renaming === s.id
      ? el("form", { class: "rename", onsubmit: (e) => {
          e.preventDefault();
          act("rename", { id: s.id, text: e.target.elements.name.value });
          renaming = "";
          render(true);
        } },
          el("input", { name: "name", value: s.title, "aria-label": "New name", "data-focus": `rename:${s.id}` }),
          el("button", { class: "button primary", type: "submit" }, "Save"),
          el("button", { class: "button", type: "button", onclick: () => { renaming = ""; render(true); } }, "Cancel"))
      : el("div", { class: "actions" },
          el("button", { class: "button primary", onclick: () => act("open", { id: s.id }, `${verb === "Open" ? "Opening" : "Resuming"} in deckk`) }, `${verb} in deckk`),
          el("button", { class: "button quiet", onclick: () => act("pin", { id: s.id }, s.pinned ? "Unpinned" : "Pinned") }, s.pinned ? "Unpin" : "Pin"),
          el("button", { class: "button quiet", onclick: () => { renaming = s.id; render(true); } }, "Rename"),
          s.running && el("button", { class: "button quiet", title: "deckk sends its safe suggestions here without asking",
            onclick: () => act("trust", { id: s.id }) }, s.trusted ? "Stop deckk sending" : "Let deckk send"),
          !s.running && el("button", { class: "button quiet", onclick: () => act("archive", { id: s.id }, s.archived ? "Unarchived" : "Archived") }, s.archived ? "Unarchive" : "Archive")),
  );
  return el("div", { class: "detail" }, el("div", { class: "detail-inner" }, body));
}

// Opening a row animates the row in place; a full redraw would skip it.
function toggleRow(row, s) {
  if (open.has(s.id)) {
    open.delete(s.id);
    row.classList.remove("open");
    row.querySelector("button").setAttribute("aria-expanded", "false");
    return;
  }
  open.add(s.id);
  loadHistory(s);
  let detail = row.querySelector(".detail");
  if (!detail) {
    detail = detailBody(s);
    row.append(detail);
  }
  void detail.offsetHeight;
  row.classList.add("open");
  row.querySelector("button").setAttribute("aria-expanded", "true");
}

function sessionRow(s) {
  const isOpen = open.has(s.id);
  const row = el("li", { class: `row${isOpen ? " open" : ""}`, "data-row": s.id });
  row.append(el("button", { "data-focus": `row:${s.id}`, "aria-expanded": isOpen ? "true" : "false", onclick: () => toggleRow(row, s) },
    el("span", { class: `dot ${s.state}`, "aria-hidden": "true" }),
    el("span", { class: "title" }, s.title,
      s.pinned && el("span", { class: "pin", "aria-label": "pinned" }, "◆"),
      s.trusted && el("span", { class: "pin", title: "deckk sends its safe suggestions here", "aria-label": "deckk sends here" }, "⇢"),
      s.suggestion && el("span", { class: "mark", title: "deckk has a suggestion", "aria-label": "has a suggestion" }, "✦"),
      s.clash && el("span", { class: "warn", title: s.clash, "aria-label": s.clash }, "⚠"),
    ),
    el("span", { class: `state ${s.state}` }, statusText(s)),
    el("span", { class: "where" }, s.project, s.branch && [" ", el("code", {}, `⎇ ${s.branch}`)], s.where && ` · ${s.where}`),
    s.summary && el("span", { class: `summary${s.stale ? " stale" : ""}` }, s.stale && el("span", { class: "earlier" }, "Earlier note: "), s.summary),
  ));
  if (isOpen) row.append(detailBody(s));
  return row;
}

// Search results carry less than the live list, so they are shaped to fit.
function fromSearch(f) {
  return {
    id: f.id, title: f.title, project: f.project, folder: f.folder, branch: f.branch, since: f.updated,
    state: f.running ? "other" : "stopped", status: f.running ? "Running" : "", lastPrompt: f.lastPrompt,
    pinned: f.pinned, archived: f.archived, running: f.running, where: f.background ? "background" : "",
  };
}

function renderGroups(s) {
  let groups;
  if (found) {
    groups = found.length ? [{ title: `${plural(found.length, "match")} for “${query}”`, sessions: found.map(fromSearch) }] : [];
  } else if (grouping === "background") {
    groups = (s.background || []).length ? [{ title: "Runs nobody typed into, such as claude -p", sessions: s.background }] : [];
  } else {
    groups = (grouping === "project" ? s.projects : s.groups) || [];
  }
  if (!groups.length) {
    $("groups").replaceChildren(el("p", { class: "groups-empty" },
      found ? "Nothing matches. Try a folder, a branch or words from a prompt."
        : grouping === "background" ? "No background runs lately."
        : grouping === "project" ? "No project has moved this week." : "No sessions yet. Press n in deckk to start one."));
    return;
  }
  $("groups").replaceChildren(...groups.map((g) =>
    el("section", { "aria-label": g.title },
      el("h3", {}, g.title),
      el("ul", { class: "card" }, g.sessions.map(sessionRow),
        g.hidden > 0 && el("li", { class: "more" }, `${g.hidden} more. Search to find them.`),
      ),
    )));
}

let searchTimer;
$("search").addEventListener("input", (e) => {
  clearTimeout(searchTimer);
  query = e.target.value.trim();
  if (!query) {
    found = null;
    render(true);
    return;
  }
  searchTimer = setTimeout(async () => {
    const asked = query;
    const response = await api(`api/search?q=${encodeURIComponent(asked)}`).catch(() => null);
    if (asked !== query) return;
    found = response && response.ok ? await response.json() : [];
    render(true);
  }, 220);
});

/* deckk: the chat with the deckk session, and the goals it keeps. */

let chatTurns = null;
let chatKey = "";
async function loadChat(force) {
  if (!state || !state.deckk || !state.deckk.id) return;
  const key = `${state.deckk.id}:${state.deckk.working}:${(state.activity || []).length}`;
  if (!force && key === chatKey) return;
  chatKey = key;
  const response = await api(`api/history?id=${encodeURIComponent(state.deckk.id)}`).catch(() => null);
  if (response && response.ok) chatTurns = await response.json();
  render(true);
}

function renderChat(s) {
  const d = s.deckk || {};
  const card = $("chat");
  if (!d.on) {
    card.replaceChildren(el("p", { class: "status" }, "The deckk session is off. Turn it on in deckk's settings: press s on the dashboard."));
    return;
  }
  if (!d.running) {
    card.replaceChildren(
      el("p", { class: "status" }, "The deckk session isn't running."),
      el("div", { class: "actions" }, el("button", { class: "button primary", onclick: () => act("talk", {}, "Starting the deckk session") }, "Start it")));
    return;
  }
  const turns = (chatTurns || []).flatMap((t) => {
    const update = t.prompt && t.prompt.startsWith("[deckk]");
    return [
      t.prompt && (update ? el("p", { class: "why" }, t.prompt.replace(/^\[deckk\]\s*/, "Update: ").replace(/\s*Check read_inbox\.?$/, "")) : el("p", { class: "bubble mine" }, t.prompt)),
      t.reply && el("p", { class: "bubble them" }, t.reply),
    ];
  });
  const chat = el("div", { class: "chat", tabindex: "0", role: "log", "data-chat": "deckk", "aria-label": "Conversation with deckk" },
    turns.length ? turns : el("p", { class: "why" }, "Ask what needs you, where you left off, or what to do next."),
    d.working && thinking("deckk is thinking"));
  keepScroll(chat, "deckk");
  card.replaceChildren(
    chat,
    composer("deckk", "Ask deckk", (text) => act("chat", { text }), { typeIn: false, text: "Enter sends, Shift+Enter adds a line. deckk answers in its pane on the dashboard too." }),
  );
}

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
  const title = el("h2", {}, "Goals",
    !editingGoals && el("button", { class: "button quiet", onclick: () => { editingGoals = true; render(true); } }, goals.trim() ? "Edit" : "Add"));
  if (editingGoals) {
    const editor = el("textarea", { class: "goals-editor", "data-focus": "goals-editor", "aria-label": "Goals",
      value: drafts.has("goals") ? drafts.get("goals") : goals, oninput: (e) => drafts.set("goals", e.target.value) });
    $("goals").replaceChildren(title, editor,
      el("div", { class: "actions" },
        el("button", { class: "button primary", onclick: () => {
          act("goals", { text: editor.value });
          drafts.delete("goals");
          editingGoals = false;
        } }, "Save"),
        el("button", { class: "button", onclick: () => { drafts.delete("goals"); editingGoals = false; render(true); } }, "Cancel")));
    return;
  }
  $("goals").replaceChildren(title,
    goals.trim()
      ? el("div", { class: "goals" }, markdown(goals))
      : el("p", {}, "No goals yet. Add some, or tell deckk what you're working toward and it will keep them here."));
}

/* Drawing it all. */

function selecting() {
  const selection = document.getSelection();
  return Boolean(selection && !selection.isCollapsed && document.querySelector("main").contains(selection.anchorNode));
}

// A redraw replaces the nodes, so it waits while the user selects text or
// types, and puts focus and scroll positions back afterwards.
function render(force = false) {
  if (!state || !state.counts) return;
  const typing = document.activeElement && ["TEXTAREA", "INPUT"].includes(document.activeElement.tagName) && document.activeElement.id !== "search";
  if (!force && (selecting() || typing)) {
    deferred = true;
    return;
  }
  deferred = false;
  const active = document.activeElement;
  const focused = active && active.dataset.focus;
  const caret = active && "selectionStart" in active ? [active.selectionStart, active.selectionEnd] : null;
  for (const chat of document.querySelectorAll("[data-chat]")) {
    scrolled.set(chat.dataset.chat, { top: chat.scrollTop, atEnd: chat.scrollTop + chat.clientHeight >= chat.scrollHeight - 4 });
  }
  const s = state;
  renderSidebar(s);
  renderHomeHead(s);
  renderClashes(s.clashes || []);
  renderNext(s.nextUp || []);
  renderNow(s);
  renderToday(s.today || {});
  renderActivity(s.activity || []);
  renderGrouping();
  renderGroups(s);
  renderChat(s);
  renderGoals(s.goals || "");
  for (const g of [...(s.groups || []), ...(s.projects || [])]) {
    for (const x of g.sessions) if (open.has(x.id)) loadHistory(x);
  }
  if (focused) {
    const again = document.querySelector(`[data-focus="${CSS.escape(focused)}"]`);
    if (again) {
      again.focus({ preventScroll: true });
      if (caret && "setSelectionRange" in again) again.setSelectionRange(caret[0], caret[1]);
    }
  }
}

document.addEventListener("selectionchange", () => { if (deferred && !selecting()) render(); });
document.addEventListener("focusout", () => setTimeout(() => { if (deferred) render(); }, 0));

function receive(data) {
  if (data === raw) return;
  raw = data;
  const first = !state;
  state = JSON.parse(data);
  const notice = state.notice || {};
  if (!first && notice.seq && notice.seq !== noticeSeen && notice.text) toast(notice.text, notice.error);
  noticeSeen = notice.seq || noticeSeen;
  render();
  if (current() === "deckk") loadChat(false);
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

showSection(false);
if (!token) status("lost", "Press w in deckk to open this page");
else stream();
setInterval(() => render(), 30000);
