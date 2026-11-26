const NOTE_FIELDS = ["summary-note", "team-note", "focus-note", "decision-note"];
const METRICS = [
  { key: "mergedPRs", label: "PRs merged", icon: "✓", unit: "", lowerIsBetter: false },
  { key: "activePRs", label: "Active pull requests", icon: "↗", unit: "", lowerIsBetter: null },
  { key: "successfulCI", label: "Successful CI runs", icon: "⌘", unit: "", lowerIsBetter: false },
  { key: "cycleTime", label: "Avg. PR cycle time", icon: "◷", unit: "hrs", lowerIsBetter: true },
  { key: "reviewCoverage", label: "Reviewed PR coverage", icon: "◎", unit: "%", lowerIsBetter: false },
];

function formatNumber(value, decimals = 0) {
  if (!Number.isFinite(value)) return "—";
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: decimals }).format(value);
}

function formatDate(value, options = { month: "short", day: "numeric" }) {
  return new Intl.DateTimeFormat(undefined, options).format(new Date(value));
}

function setText(id, value) {
  document.getElementById(id).textContent = value;
}

function appendWorkItem(container, item, completed) {
  const row = document.createElement("article");
  row.className = "work-item";
  const bullet = document.createElement("span");
  bullet.className = "work-bullet";
  bullet.textContent = completed ? "✓" : "↗";
  const copy = document.createElement("div");
  copy.className = "work-copy";
  const title = document.createElement(item.url && item.url !== "#" ? "a" : "span");
  title.className = "work-title";
  title.textContent = item.title;
  if (title instanceof HTMLAnchorElement) {
    title.href = item.url;
    title.target = "_blank";
    title.rel = "noopener noreferrer";
  }
  const meta = document.createElement("div");
  meta.className = "work-meta";
  const source = document.createElement("span");
  source.className = "source-tag";
  source.textContent = item.source || "Engineering";
  meta.append(source);
  if (item.author) {
    const author = document.createElement("span");
    author.textContent = `· ${item.author}`;
    meta.append(author);
  }
  if (completed && item.closedAt) {
    const date = document.createElement("span");
    date.textContent = `· merged ${formatDate(item.closedAt)}`;
    meta.append(date);
  }
  copy.append(title, meta);
  row.append(bullet, copy);
  container.append(row);
}

function renderMetric(metric, definition) {
  const card = document.createElement("article");
  card.className = "metric-card";
  const top = document.createElement("div");
  top.className = "metric-top";
  const label = document.createElement("span");
  label.textContent = definition.label;
  const icon = document.createElement("span");
  icon.className = "metric-icon";
  icon.textContent = definition.icon;
  top.append(label, icon);
  const value = document.createElement("div");
  value.className = "metric-value";
  const amount = metric?.value;
  value.append(document.createTextNode(formatNumber(amount, metric.key === "cycleTime" ? 1 : 0)));
  if (definition.unit) {
    const unit = document.createElement("small");
    unit.textContent = definition.unit;
    value.append(unit);
  }
  const trend = document.createElement("div");
  trend.className = "metric-trend";
  if (!Number.isFinite(metric.value) || !Number.isFinite(metric.previous)) {
    trend.textContent = "No data available";
    card.append(top, value, trend);
    return card;
  }
  const delta = metric.value - metric.previous;
  const deltaUnit = metric.key === "reviewCoverage" ? " pp" : metric.key === "cycleTime" ? " hrs" : "";
  const deltaText = `${delta > 0 ? "+" : ""}${formatNumber(delta, metric.key === "cycleTime" ? 1 : 0)}${deltaUnit} vs last week`;
  trend.textContent = deltaText;
  if (definition.lowerIsBetter !== null && delta !== 0) {
    const improved = definition.lowerIsBetter ? delta < 0 : delta > 0;
    trend.classList.add(improved ? "trend-good" : "trend-bad");
  }
  card.append(top, value, trend);
  return card;
}

function renderSources(sources, demo) {
  const root = document.getElementById("source-list");
  root.replaceChildren();
  const entries = sources.length ? sources : [
    { name: "GitHub Enterprise", kind: "GitHub Enterprise", connected: false, message: "Not configured" },
    { name: "Azure DevOps", kind: "Azure DevOps", connected: false, message: "Not configured" },
  ];
  for (const source of entries) {
    const card = document.createElement("article");
    card.className = "source-card";
    const logo = document.createElement("span");
    logo.className = "source-logo";
    logo.textContent = source.kind.startsWith("Azure") ? "Az" : "GH";
    const info = document.createElement("div");
    info.className = "source-info";
    const name = document.createElement("div");
    name.className = "source-name";
    name.textContent = source.name;
    const detail = document.createElement("div");
    detail.className = "source-detail";
    detail.textContent = source.message;
    info.append(name, detail);
    const state = document.createElement("span");
    const connected = source.connected === true;
    state.className = `source-state ${connected ? "connected" : source.message === "Unable to load data" ? "failed" : ""}`;
    const dot = document.createElement("span");
    dot.className = "state-dot";
    const label = document.createElement("span");
    label.textContent = demo ? "Preview" : connected ? "Connected" : "Not connected";
    state.append(dot, label);
    card.append(logo, info, state);
    root.append(card);
  }
}

function renderReport(report) {
  document.getElementById("demo-notice").hidden = !report.demo;
  const errors = document.getElementById("error-notice");
  errors.hidden = !report.errors?.length;
  errors.textContent = report.errors?.length ? `Some source data could not be loaded: ${report.errors.join(" · ")}` : "";
  setText("report-period", `${formatDate(report.weekStart)} – ${formatDate(new Date(new Date(report.weekEnd).getTime() - 1), { month: "short", day: "numeric", year: "numeric" })}`);
  setText("summary-date", `Week of ${formatDate(report.weekStart, { month: "long", day: "numeric" })}`);
  setText("updated-at", `Updated ${new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(new Date(report.generatedAt))}`);

  const metrics = document.getElementById("metrics");
  metrics.replaceChildren(...METRICS.map((definition) => {
    const metric = report.metrics?.[definition.key] ?? { value: NaN, previous: NaN };
    return renderMetric({ ...metric, key: definition.key }, definition);
  }));
  const shipped = document.getElementById("shipped-list");
  const active = document.getElementById("progress-list");
  shipped.replaceChildren();
  active.replaceChildren();
  for (const item of report.shipped ?? []) appendWorkItem(shipped, item, true);
  for (const item of report.inProgress ?? []) appendWorkItem(active, item, false);
  setText("shipped-count", String(report.shipped?.length ?? 0));
  setText("progress-count", String(report.inProgress?.length ?? 0));
  document.getElementById("shipped-empty").hidden = Boolean(report.shipped?.length);
  document.getElementById("progress-empty").hidden = Boolean(report.inProgress?.length);

  const risks = document.getElementById("risk-content");
  risks.replaceChildren();
  for (const item of report.risks ?? []) {
    const row = document.createElement("article");
    row.className = "risk-item";
    const symbol = document.createElement("span");
    symbol.className = "risk-indicator";
    symbol.textContent = "!";
    const copy = document.createElement("div");
    copy.className = "risk-copy";
    const title = document.createElement("strong");
    title.textContent = item.title;
    const description = document.createElement("p");
    const ageDays = Math.floor((Date.now() - new Date(item.createdAt).getTime()) / 86400000);
    description.textContent = `Open for ${ageDays} days · Review for possible blocker`;
    copy.append(title, description);
    row.append(symbol, copy);
    risks.append(row);
  }
  setText("risk-count", `${report.risks?.length ?? 0} ${report.risks?.length === 1 ? "item" : "items"} to review`);
  document.getElementById("risk-empty").hidden = Boolean(report.risks?.length);
  renderSources(report.sources ?? [], report.demo);
}

function loadNotes() {
  for (const id of NOTE_FIELDS) {
    const field = document.getElementById(id);
    field.value = localStorage.getItem(`engineering-status:${id}`) || "";
    field.addEventListener("input", () => localStorage.setItem(`engineering-status:${id}`, field.value));
  }
}

function reportMarkdown(report) {
  const notes = (id) => document.getElementById(id).value.trim() || "—";
  const metricLines = METRICS.map((definition) => {
    const metric = report.metrics?.[definition.key] ?? { value: 0, previous: 0 };
    const unit = definition.unit ? ` ${definition.unit}` : "";
    return `| ${definition.label} | ${formatNumber(metric.value, definition.key === "cycleTime" ? 1 : 0)}${unit} | ${formatNumber(metric.previous, definition.key === "cycleTime" ? 1 : 0)}${unit} |`;
  });
  const list = (items) => items?.length ? items.map((item) => `- ${item.title} (${item.source})`).join("\n") : "- None this week";
  return [
    `# Engineering weekly status — week of ${formatDate(report.weekStart, { month: "long", day: "numeric", year: "numeric" })}`,
    "", "## Executive summary", notes("summary-note"),
    "", "## Key metrics", "| Metric | This week | Last week |", "|---|---:|---:|", ...metricLines,
    "", "## Shipped this week", list(report.shipped),
    "", "## In progress", list(report.inProgress),
    "", "## Risks & blockers", list(report.risks),
    "", "## Team updates", notes("team-note"),
    "", "## Next week’s focus", notes("focus-note"),
    "", "## Decisions needed", notes("decision-note"),
  ].join("\n");
}

let latestReport;
async function refreshReport() {
  const button = document.getElementById("refresh");
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  try {
    const response = await fetch("/api/status", { cache: "no-store" });
    if (!response.ok) throw new Error(`Request failed (${response.status})`);
    latestReport = await response.json();
    renderReport(latestReport);
  } catch (error) {
    const notice = document.getElementById("error-notice");
    notice.hidden = false;
    notice.textContent = `Could not load the status report: ${error.message}`;
  } finally {
    button.disabled = false;
    button.removeAttribute("aria-busy");
  }
}

loadNotes();
document.getElementById("refresh").addEventListener("click", refreshReport);
document.getElementById("copy-report").addEventListener("click", async () => {
  if (!latestReport) return;
  const button = document.getElementById("copy-report");
  try {
    await navigator.clipboard.writeText(reportMarkdown(latestReport));
    button.textContent = "Copied ✓";
    setTimeout(() => { button.innerHTML = 'Copy report <span aria-hidden="true">↗</span>'; }, 1600);
  } catch {
    button.textContent = "Copy unavailable";
    setTimeout(() => { button.innerHTML = 'Copy report <span aria-hidden="true">↗</span>'; }, 1600);
  }
});
refreshReport();
