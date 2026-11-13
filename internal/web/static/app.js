"use strict";

// deckk's link carries the token after #, which never reaches a server. It
// is kept per origin, which includes the port, and cleared from the address.
let token = "";
function readToken() {
  const fromLink = new URLSearchParams(location.hash.slice(1)).get("t");
  try {
    if (fromLink) localStorage.setItem("deckk-token", fromLink);
    token = fromLink || localStorage.getItem("deckk-token") || "";
  } catch {
    token = fromLink || token;
  }
  if (fromLink) history.replaceState(null, "", location.pathname);
  return Boolean(fromLink);
}
readToken();

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
let starting = false;
let closing = "";
let pendingChat = null;
let clashKey = "";
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
  if (!response) {
    toast("deckk isn't answering. Is it still running?", true);
    return false;
  }
  if (!response.ok) {
    toast((await response.text()).trim(), true);
    return false;
  }
  if (done) toast(done);
  return true;
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

window.addEventListener("hashchange", () => {
  if (readToken()) stream();
  showSection(true);
});

function go(section, sessionID) {
  if (sessionID) {
    open.add(sessionID);
    clearTimeout(searchTimer);
    query = "";
    $("search").value = "";
    found = null;
    const listed = (grouping === "project" ? state.projects : grouping === "background" ? [{ sessions: state.background }] : state.groups) || [];
    if (!listed.some((g) => (g.sessions || []).some((x) => x.id === sessionID))) grouping = "status";
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

function renderSidebar(s) {
  const waiting = s.counts.waiting;
  const home = $("count-home");
  const count = (node, n, label) => {
    node.textContent = n || "";
    if (n) node.setAttribute("aria-label", label);
    else node.removeAttribute("aria-label");
  };
  count(home, waiting, `${waiting} waiting on you`);
  home.classList.toggle("alert", waiting > 0);
  const live = s.counts.waiting + s.counts.working;
  count($("count-sessions"), live, `${live} running`);
  const next = (s.nextUp || []).length;
  count($("count-deckk"), next, `${plural(next, "suggestion")}`);
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
  const key = JSON.stringify(clashes);
  if (key === clashKey) return;
  clashKey = key;
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
    $("now").replaceChildren(el("div", { class: "empty-card" },
      el("p", {}, "Nothing is running. Start a session, or resume one from Sessions."),
      el("button", { class: "button primary", "data-focus": "start-empty", onclick: () => newSession() }, "New session")));
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

function renderGrouping() {
  const choice = (value, label) => el("button", {
    class: "segment", "aria-pressed": !found && grouping === value ? "true" : "false", "data-focus": `grouping:${value}`,
    disabled: Boolean(found), title: found ? "Clear the search to group sessions" : undefined,
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
  if (!response || !response.ok) {
    histories.delete(s.id);
    return;
  }
  const turns = await response.json();
  histories.set(s.id, { since: s.since, turns });
  patchChat(s);
}

// A loaded history replaces only the conversation, so an opening row keeps
// animating and the composer keeps its text.
function patchChat(s) {
  const old = document.querySelector(`[data-chat="${CSS.escape(s.id)}"]`);
  if (!old) return render();
  if (selecting()) {
    deferred = true;
    return;
  }
  saveScroll(old);
  const fresh = conversation(s);
  if (fresh) old.replaceWith(fresh);
}

function conversation(s) {
  const loaded = histories.get(s.id);
  const turns = loaded && loaded.turns && loaded.turns.length ? loaded.turns : [{ prompt: s.lastPrompt, reply: s.lastReply }];
  const chat = el("div", { class: "chat", tabindex: "0", role: "region", "data-chat": s.id, "aria-label": `Recent conversation in ${s.title}` },
    turns.flatMap((t) => [
      t.prompt && el("p", { class: "bubble mine" }, t.prompt),
      t.reply && el("p", { class: "bubble them" }, t.reply),
    ]),
    s.state === "working" && thinking("Claude is working"));
  if (!chat.childElementCount) return null;
  keepScroll(chat, s.id);
  return chat;
}

function thinking(label) {
  return el("p", { class: "bubble them thinking", role: "img", "aria-label": label }, el("span", { class: "typing", "aria-hidden": "true" }, el("i"), el("i"), el("i")));
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
  const offer = hint.suggestion;
  const fill = () => {
    area.value = offer.text;
    drafts.set(key, offer.text);
    area.focus();
    area.setSelectionRange(area.value.length, area.value.length);
  };
  const area = el("textarea", {
    rows: "1", placeholder, "data-focus": `compose:${key}`, "aria-label": placeholder,
    value: drafts.get(key) || "", disabled: Boolean(hint.disabled),
    oninput: (e) => drafts.set(key, e.target.value),
    onkeydown: (e) => {
      if (e.key === "Tab" && !e.shiftKey && offer && !area.value) {
        e.preventDefault();
        fill();
        return;
      }
      // Safari reports the Enter that picks an IME candidate as keyCode 229.
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing && e.keyCode !== 229) {
        e.preventDefault();
        submit(!(hint.typeIn && e.altKey));
      }
    },
  });
  const submit = async (send) => {
    const text = area.value.trim();
    if (!text) return;
    drafts.delete(key);
    area.value = "";
    if (!(await onSubmit(text, send))) {
      drafts.set(key, text);
      const now = document.querySelector(`[data-focus="${CSS.escape(`compose:${key}`)}"]`);
      if (now && !now.value) now.value = text;
    }
  };
  return el("div", { class: "composer" },
    offer && el("div", { class: "offer-bar" },
      el("button", { class: `offered${offer.ready ? " ready" : ""}`, "data-focus": `offer:${key}`, title: "Put this in the message box to edit or send", onclick: fill },
        el("span", { class: "offered-label" }, offer.ready ? "✦ deckk typed this into the session's prompt" : "✦ deckk suggests"),
        el("span", { class: "offered-text" }, offer.text),
        offer.why && el("span", { class: "offered-why" }, offer.why)),
      el("button", { class: "offer-dismiss", "data-focus": `offer-x:${key}`, "aria-label": "Dismiss deckk's suggestion", title: "Dismiss", onclick: offer.dismiss }, "✕")),
    el("div", { class: "field-row" },
      area,
      el("button", { class: "send", "data-focus": `send:${key}`, disabled: Boolean(hint.disabled), "aria-label": hint.sendLabel || "Send", title: hint.sendLabel || "Send", onclick: () => submit(true) },
        hint.sendLabel ? hint.sendLabel : el("span", { class: "send-arrow", "aria-hidden": "true" }))),
    el("p", { class: "composer-hint" }, hint.text,
      hint.typeIn && !hint.disabled && [" ", el("button", { class: "link", "data-focus": `typein:${key}`, onclick: () => submit(false) }, "Type in without sending"),
        el("span", { class: "keys" }, " ⌥↵")]),
  );
}

function detailBody(s) {
  const verb = s.state === "stopped" ? "Resume" : "Open";
  const offer = s.suggestion && { text: s.suggestion, why: s.why, ready: s.ready, dismiss: () => act("dismiss", { id: s.id }) };
  const body = el("div", { class: "detail-body" },
    s.clash && el("p", { class: "note" }, `⚠ ${s.clash}`),
    conversation(s),
    composer(s.id, s.running ? `Message ${s.title}` : `Resume ${s.title} with a message`,
      (text, send) => act("prompt", { id: s.id, text, send }),
      s.running && !s.hosted
        ? { typeIn: true, disabled: true, suggestion: offer, text: "This session runs in another terminal tab. Press m on it in deckk to move it in, then write to it here." }
        : { typeIn: true, suggestion: offer, text: offer ? "Click the suggestion or press Tab to use it." : "Enter sends." }),
    el("div", { class: "facts" },
      el("span", {}, s.folder),
      s.pr && safeLink(s.prUrl) && el("a", { href: s.prUrl, target: "_blank", rel: "noopener noreferrer" }, `PR ${s.pr}`),
      s.model && el("span", {}, s.model),
      s.context && el("span", {}, `${Math.round(s.context / 1000)}k context`),
      s.changed && el("span", {}, plural(s.changed, "uncommitted change")),
    ),
    renaming === s.id
      ? el("form", { class: "rename", onsubmit: async (e) => {
          e.preventDefault();
          const name = e.target.elements.name.value;
          renaming = "";
          drafts.delete(`rename:${s.id}`);
          render(true);
          if (await act("rename", { id: s.id, text: name })) refreshSearch();
          focusOn(`more:${s.id}`);
        } },
          el("input", { name: "name", value: drafts.get(`rename:${s.id}`) ?? s.title, "aria-label": "New name", "data-focus": `rename:${s.id}`,
            oninput: (e) => drafts.set(`rename:${s.id}`, e.target.value),
            onkeydown: (e) => { if (e.key === "Escape") { renaming = ""; drafts.delete(`rename:${s.id}`); render(true); focusOn(`more:${s.id}`); } } }),
          el("button", { class: "button primary", type: "submit" }, "Save"),
          el("button", { class: "button", type: "button", onclick: () => { renaming = ""; drafts.delete(`rename:${s.id}`); render(true); focusOn(`more:${s.id}`); } }, "Cancel"))
      : el("div", { class: "actions" },
          el("button", { class: "button primary", "data-focus": `go:${s.id}`, onclick: () => act("open", { id: s.id }, `${verb === "Open" ? "Opening" : "Resuming"} in deckk`) }, `${verb} in deckk`),
          el("button", { class: "button quiet", "data-focus": `pin:${s.id}`, onclick: async () => { if (await act("pin", { id: s.id }, s.pinned ? "Unpinned" : "Pinned")) refreshSearch(); } }, s.pinned ? "Unpin" : "Pin"),
          el("button", { class: "button quiet", "data-focus": `more:${s.id}`, onclick: () => { renaming = s.id; render(true); focusOn(`rename:${s.id}`, true); } }, "Rename"),
          s.closable && el("button", { class: `button quiet${closing === s.id ? " danger" : ""}`, "data-focus": `close:${s.id}`,
            onclick: () => closeSession(s) }, closing === s.id ? "Claude is working. Close anyway" : "Close"),
          !s.running && el("button", { class: "button quiet", "data-focus": `archive:${s.id}`, onclick: async () => { if (await act("archive", { id: s.id }, s.archived ? "Unarchived" : "Archived")) refreshSearch(); } }, s.archived ? "Unarchive" : "Archive"),
          s.running && el("button", {
            class: "switch", role: "switch", "aria-checked": s.trusted ? "true" : "false", "data-focus": `trust:${s.id}`,
            title: "When deckk judges its suggestion safe, it sends it here without waiting for you",
            onclick: () => act("trust", { id: s.id }, s.trusted ? "deckk will wait for you here" : "deckk sends its safe suggestions here"),
          }, el("span", { class: "track", "aria-hidden": "true" }), "Auto-send deckk's suggestions")),
  );
  return el("div", { class: "detail" }, el("div", { class: "detail-inner" }, body));
}

// Opening a row animates the row in place; a full redraw would skip it.
function toggleRow(row, s) {
  if (open.has(s.id)) {
    open.delete(s.id);
    row.classList.remove("open");
    row.querySelector("button").setAttribute("aria-expanded", "false");
    const detail = row.querySelector(".detail");
    if (detail) detail.inert = true;
    return;
  }
  open.add(s.id);
  loadHistory(s);
  let detail = row.querySelector(".detail");
  if (!detail) {
    detail = detailBody(s);
    row.append(detail);
  }
  detail.inert = false;
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
      s.pinned && el("span", { class: "pin", role: "img", "aria-label": "pinned" }, "◆"),
      s.trusted && el("span", { class: "pin", role: "img", title: "deckk sends its safe suggestions here", "aria-label": "deckk sends here" }, "⇢"),
      s.suggestion && el("span", { class: "mark", role: "img", title: "deckk has a suggestion", "aria-label": "has a suggestion" }, "✦"),
      s.clash && el("span", { class: "warn", role: "img", title: s.clash, "aria-label": s.clash }, "⚠"),
    ),
    el("span", { class: `state ${s.state}` }, statusText(s)),
    el("span", { class: "where" }, s.project, s.branch && [" ", el("code", {}, `⎇ ${s.branch}`)], s.where && ` · ${s.where}`),
    s.summary && el("span", { class: `summary${s.stale ? " stale" : ""}` }, s.stale && el("span", { class: "earlier" }, "Earlier note: "), s.summary),
  ));
  if (isOpen) row.append(detailBody(s));
  return row;
}

// Search results carry less than the live list, so they are shaped to fit,
// unless the dashboard lists the session too.
function fromSearch(f) {
  const live = [...(state.groups || []), { sessions: state.background || [] }].flatMap((g) => g.sessions).find((x) => x.id === f.id);
  if (live) return live;
  return {
    id: f.id, title: f.title, project: f.project, folder: f.folder, branch: f.branch, since: f.updated,
    state: f.running ? "other" : "stopped", status: f.running ? "Running" : "", lastPrompt: f.lastPrompt,
    pinned: f.pinned, archived: f.archived, running: f.running, where: f.background ? "background" : "",
  };
}

function renderGroups(s) {
  let groups;
  if (found) {
    const named = found.filter((f) => !f.mentioned);
    const said = found.filter((f) => f.mentioned);
    groups = [
      named.length && { title: `${plural(named.length, "match")} for “${query}”`, sessions: named.map(fromSearch) },
      said.length && { title: "Mentioned in the conversation", sessions: said.map(fromSearch) },
    ].filter(Boolean);
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
  searchTimer = setTimeout(runSearch, 220);
});

async function runSearch() {
  const asked = query;
  if (!asked) return;
  const response = await api(`api/search?q=${encodeURIComponent(asked)}`).catch(() => null);
  const results = response && response.ok ? await response.json() : [];
  if (asked !== query) return;
  found = results;
  render(true);
}

function refreshSearch() {
  if (found) setTimeout(runSearch, 300);
}

function newSession() {
  starting = true;
  if (current() !== "sessions") location.hash = "sessions";
  render(true);
  focusOn("new-folder", true);
}

function renderNew(s) {
  const panel = $("new-session");
  panel.hidden = !starting;
  $("new-button").setAttribute("aria-expanded", starting ? "true" : "false");
  if (!starting) {
    panel.replaceChildren();
    return;
  }
  const folders = el("datalist", { id: "folders" }, (s.folders || []).map((f) => el("option", { value: f })));
  const folder = el("input", {
    name: "folder", list: "folders", placeholder: "~/dev/app", autocomplete: "off", spellcheck: "false",
    "data-focus": "new-folder", "aria-label": "Folder", value: drafts.get("new-folder") || "",
    oninput: (e) => drafts.set("new-folder", e.target.value),
  });
  const cancel = () => {
    starting = false;
    drafts.delete("new-folder");
    drafts.delete("new-prompt");
    render(true);
    focusOn("new-button");
  };
  panel.replaceChildren(
    el("h2", {}, "New session"),
    el("label", { class: "field" }, el("span", {}, "Folder"), folder, folders),
    el("p", { class: "composer-hint" }, "Pick a folder you've worked in, or type a new path to create it."),
    composer("new-prompt", "First prompt (optional)", async (text) => start(text), {
      typeIn: false, sendLabel: "Start", text: "Claude starts in deckk and gets this prompt once it's ready.",
    }),
    el("div", { class: "actions" },
      el("button", { class: "button", "data-focus": "new-cancel", onclick: cancel }, "Cancel"),
      el("button", { class: "button primary", "data-focus": "new-start", onclick: () => start("") }, "Start without a prompt")),
  );
  async function start(prompt) {
    const dir = folder.value.trim();
    if (!dir) {
      toast("Pick a folder to start in", true);
      folder.focus();
      return false;
    }
    if (!(await act("new", { folder: dir, text: prompt, send: true }))) return false;
    starting = false;
    drafts.delete("new-folder");
    drafts.delete("new-prompt");
    render(true);
    return true;
  }
}

$("new-button").addEventListener("click", () => (starting ? (starting = false, render(true)) : newSession()));

async function closeSession(s) {
  if (s.state === "working" && closing !== s.id) {
    closing = s.id;
    render(true);
    focusOn(`close:${s.id}`);
    return;
  }
  closing = "";
  await act("close", { id: s.id, confirm: s.state === "working" }, `Closed ${s.title}. It stays resumable`);
}

let chatTurns = null;
let chatKey = "";
async function loadChat(force) {
  if (!state || !state.deckk || !state.deckk.id) return;
  const key = `${state.deckk.id}:${state.deckk.working}:${(state.activity || []).length}`;
  if (!force && key === chatKey) return;
  chatKey = key;
  const response = await api(`api/history?id=${encodeURIComponent(state.deckk.id)}`).catch(() => null);
  if (!response || !response.ok) {
    chatKey = "";
    return;
  }
  const turns = await response.json();
  if (key !== chatKey) return;
  chatTurns = turns;
  if (pendingChat && turns.some((t) => t.prompt && t.prompt.trim() === pendingChat.text)) pendingChat = null;
  render();
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
  if (pendingChat && Date.now() - pendingChat.at > 120000) pendingChat = null;
  const chat = el("div", { class: "chat", tabindex: "0", role: "region", "data-chat": "deckk", "aria-label": "Conversation with deckk" },
    turns.length || pendingChat ? turns : chatTurns && el("p", { class: "why" }, "Ask what needs you, where you left off, or what to do next."),
    pendingChat && el("p", { class: "bubble mine pending" }, pendingChat.text),
    (d.working || pendingChat) && thinking("deckk is thinking"));
  keepScroll(chat, "deckk");
  card.replaceChildren(
    chat,
    composer("deckk", "Ask deckk", async (text) => {
      if (!(await act("chat", { text }))) return false;
      pendingChat = { text, at: Date.now() };
      render(true);
      return true;
    }, { typeIn: false, text: "Enter sends, Shift+Enter adds a line. deckk answers in its pane on the dashboard too." }),
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
    !editingGoals && el("button", { class: "button quiet", "data-focus": "goals-edit", onclick: () => { editingGoals = true; render(true); focusOn("goals-editor", true); } }, goals.trim() ? "Edit" : "Add"));
  if (editingGoals) {
    const editor = el("textarea", { class: "goals-editor", "data-focus": "goals-editor", "aria-label": "Goals",
      value: drafts.has("goals") ? drafts.get("goals") : goals, oninput: (e) => drafts.set("goals", e.target.value) });
    $("goals").replaceChildren(title, editor,
      el("div", { class: "actions" },
        el("button", { class: "button primary", "data-focus": "goals-save", onclick: async () => {
          if (!(await act("goals", { text: editor.value }))) return;
          state.goals = editor.value;
          drafts.delete("goals");
          editingGoals = false;
          render(true);
          focusOn("goals-edit");
        } }, "Save"),
        el("button", { class: "button", "data-focus": "goals-cancel", onclick: () => { drafts.delete("goals"); editingGoals = false; render(true); focusOn("goals-edit"); } }, "Cancel")));
    return;
  }
  $("goals").replaceChildren(title,
    goals.trim()
      ? el("div", { class: "goals" }, markdown(goals))
      : el("p", {}, "No goals yet. Add some, or tell deckk what you're working toward and it will keep them here."));
}

let selectedAt = 0;
function selecting() {
  const selection = document.getSelection();
  const now = Boolean(selection && !selection.isCollapsed && document.querySelector("main").contains(selection.anchorNode));
  if (!now) selectedAt = 0;
  else if (!selectedAt) selectedAt = Date.now();
  // A selection left behind stops holding the page after a while.
  return now && Date.now() - selectedAt < 20000;
}

let composing = false;
document.addEventListener("compositionstart", () => { composing = true; });
document.addEventListener("compositionend", () => { composing = false; });

function typing() {
  const active = document.activeElement;
  if (!active || !["TEXTAREA", "INPUT"].includes(active.tagName) || active.id === "search") return false;
  return composing || active.value !== "";
}

// A press that blurs a field must reach its button before a waiting redraw
// replaces it, so redraws hold until the pointer is up.
let pointerDown = false;
document.addEventListener("pointerdown", () => { pointerDown = true; }, true);
document.addEventListener("pointerup", () => {
  pointerDown = false;
  setTimeout(() => { if (deferred) render(); }, 0);
}, true);
document.addEventListener("pointercancel", () => { pointerDown = false; }, true);

function focusOn(key, select = false) {
  requestAnimationFrame(() => {
    const node = document.querySelector(`[data-focus="${CSS.escape(key)}"]`);
    if (!node) return;
    node.focus({ preventScroll: true });
    if (select && "select" in node && node.tagName === "INPUT") node.select();
  });
}

function saveScroll(chat) {
  // Hidden chats measure zero and would be saved as scrolled to the end.
  if (!chat.clientHeight) return;
  scrolled.set(chat.dataset.chat, { top: chat.scrollTop, atEnd: chat.scrollTop + chat.clientHeight >= chat.scrollHeight - 4 });
}

// A redraw replaces the nodes, so it waits while the user selects text,
// types or presses, and puts focus and scroll positions back afterwards.
function render(force = false) {
  if (!state || !state.counts) return;
  if (!force && (selecting() || typing() || pointerDown)) {
    deferred = true;
    return;
  }
  deferred = false;
  const active = document.activeElement;
  const focused = active && active.dataset.focus;
  const caret = active && "selectionStart" in active ? [active.selectionStart, active.selectionEnd] : null;
  for (const chat of document.querySelectorAll("[data-chat]")) saveScroll(chat);
  if (closing && !(state.groups || []).some((g) => g.sessions.some((x) => x.id === closing && x.state === "working"))) closing = "";
  const s = state;
  renderSidebar(s);
  renderHomeHead(s);
  renderClashes(s.clashes || []);
  renderNext(s.nextUp || []);
  renderNow(s);
  renderToday(s.today || {});
  renderActivity(s.activity || []);
  renderNew(s);
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
document.addEventListener("focusout", () => setTimeout(() => { if (deferred && !pointerDown) render(); }, 0));

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

function status(kind, text, banner = "") {
  const live = $("live");
  live.dataset.state = kind;
  live.textContent = text;
  const offline = $("offline");
  offline.textContent = banner;
  offline.hidden = !banner;
  document.body.toggleAttribute("data-offline", kind === "lost" && Boolean(state));
}

const noToken = "Open this page from deckk: press w on the dashboard, or click Web ↗.";

// The live stream is read with fetch rather than EventSource, so it can send
// the token as a header like every other request.
let streams = 0;
async function stream() {
  const mine = ++streams;
  for (let wait = 1000; mine === streams; wait = Math.min(wait * 2, 15000)) {
    const response = await api("api/stream").catch(() => null);
    if (mine !== streams) {
      if (response && response.body) response.body.cancel().catch(() => {});
      return;
    }
    if (response && response.status === 401) {
      try { localStorage.removeItem("deckk-token"); } catch {}
      status("lost", "Not connected", noToken);
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
          if (done || mine !== streams) break;
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
    if (mine !== streams) return;
    status("lost", response ? "Reconnecting" : "deckk isn't running",
      state ? "deckk isn't answering, so this page may be out of date. It reconnects on its own once deckk runs again." : "");
    await new Promise((resolve) => setTimeout(resolve, wait));
  }
}

showSection(false);
if (!token) status("lost", "Not connected", noToken);
else stream();
setInterval(() => render(), 30000);
