// Demo data for the hub screenshot. script/screenshot loads it after app.js,
// so the image never shows anyone's real sessions.
const now = Date.now();
const minutesAgo = (minutes) => new Date(now - minutes * 60000).toISOString();
const session = (fields) => Object.assign({ state: "stopped", since: minutesAgo(60), running: false }, fields);

state = {
  counts: { waiting: 2, working: 1, resumable: 6 },
  clashes: [],
  nextUp: [
    { id: "a", kind: "suggestion", title: "Checkout flow redesign", project: "shop", prompt: "Run the payment tests again now that the cursor change is merged", why: "The API session merged #212", ready: true },
    { id: "p", kind: "proposal", title: "New session in mobile", project: "~/dev/mobile", prompt: "Fix the share sheet upload crash on iOS", why: "It blocks this week's release" },
  ],
  groups: [{ title: "Running", sessions: [
    session({ id: "a", title: "Checkout flow redesign", project: "shop", branch: "checkout", state: "waiting", since: minutesAgo(4), running: true, suggestion: "Run the payment tests again now that the cursor change is merged" }),
    session({ id: "b", title: "Search API pagination", project: "api", branch: "search-cursor", state: "working", since: minutesAgo(3), running: true, summary: "Moving search to cursor pagination; tests half done" }),
    session({ id: "c", title: "Onboarding copy pass", project: "site", branch: "main", state: "waiting", since: minutesAgo(25), running: true, summary: "Copy drafted for three screens, waiting on your review" }),
  ] }],
  projects: [],
  background: [],
  goals: "",
  activity: [
    { at: minutesAgo(2), text: "Search API pagination opened a PR: #212" },
    { at: minutesAgo(9), text: "Checkout flow redesign finished" },
    { at: minutesAgo(31), text: "Release notes draft closed" },
  ],
  today: { touched: 6, prs: 2, projects: 3, busiest: "shop" },
  deckk: { on: true, running: true, working: false, id: "deckk" },
  notice: {},
};
history.replaceState(null, "", "#home");
showSection(false);
render(true);
status("live", "Live");
