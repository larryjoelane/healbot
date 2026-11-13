package ui

const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Healbot</title>
<style>
:root {
  --bg: #121612;
  --panel: #1c241e;
  --panel-2: #242e26;
  --line: #314036;
  --text: #e7f2e6;
  --muted: #93a396;
  --lime: #d6f56a;
  --lime-ink: #1a2408;
  --amber: #f0b429;
  --red: #ff6d57;
  --blue: #9fd4ff;
  --ok: #8dce78;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font: 15px/1.45 "Avenir Next", "Segoe UI", sans-serif;
}
a { color: var(--blue); text-decoration: none; }
a:hover { text-decoration: underline; }
header {
  display: flex; gap: 16px; align-items: center; justify-content: space-between;
  padding: 16px 22px; border-bottom: 1px solid var(--line); background: #161c17;
}
.brand { display: flex; gap: 12px; align-items: center; }
.mark { width: 14px; height: 28px; background: var(--lime); }
.brand h1 { font-size: 15px; letter-spacing: 0.16em; margin: 0; font-weight: 700; }
.brand p { margin: 0; color: var(--muted); font-size: 12px; }
nav { display: flex; gap: 14px; align-items: center; }
nav a { color: var(--text); font-size: 13px; }
.pills { display: flex; gap: 8px; flex-wrap: wrap; }
.pill { border: 1px solid var(--line); border-radius: 999px; padding: 3px 10px; font-size: 12px; color: var(--muted); }
.pill.on { background: var(--lime); color: var(--lime-ink); border-color: var(--lime); font-weight: 700; }
.pill.bad { background: var(--red); color: #2a0906; border-color: var(--red); font-weight: 700; }
main { padding: 22px; max-width: 1100px; margin: 0 auto; }
.banner {
  background: var(--panel); border: 1px solid var(--line); padding: 14px 16px; margin-bottom: 16px;
  display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap;
}
.banner strong { font-family: ui-monospace, "SFMono-Regular", monospace; font-size: 13px; }
button, .button {
  background: var(--lime); color: var(--lime-ink); border: 0; padding: 8px 12px; font-weight: 700;
  cursor: pointer; font: inherit;
}
button.quiet { background: transparent; color: var(--text); border: 1px solid var(--line); }
table { width: 100%; border-collapse: collapse; background: var(--panel); }
th, td { text-align: left; padding: 10px 12px; border-bottom: 1px solid var(--line); vertical-align: top; }
th { color: var(--muted); font-size: 12px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; }
.sev { font-family: ui-monospace, monospace; font-size: 12px; }
.sev-0, .sev-1 { color: var(--red); }
.sev-2 { color: var(--amber); }
.grid { display: grid; grid-template-columns: 1.4fr 0.8fr; gap: 16px; }
.card { background: var(--panel); border: 1px solid var(--line); padding: 14px 16px; margin-bottom: 12px; }
.card h2 { margin: 0 0 8px; font-size: 14px; letter-spacing: 0.04em; text-transform: uppercase; color: var(--muted); }
pre { white-space: pre-wrap; word-break: break-word; font: 12px/1.45 ui-monospace, monospace; background: var(--panel-2); padding: 10px; margin: 0; }
.muted { color: var(--muted); }
.row { display: flex; gap: 8px; flex-wrap: wrap; }
@media (max-width: 800px) {
  header, .banner, .grid { display: block; }
  nav, .pills { margin-top: 10px; }
  table { display: block; overflow-x: auto; }
}
</style>
</head>
<body>
<header>
  <div class="brand">
    <div class="mark" aria-hidden="true"></div>
    <div>
      <h1>HEALBOT</h1>
      <p>Azure incident control</p>
    </div>
  </div>
  <nav>
    <a href="/">Incidents</a>
    <a href="/audit">Audit</a>
  </nav>
  <div class="pills">
    <span class="pill">{{.Mode}}</span>
    {{if .DryRun}}<span class="pill on">Dry run</span>{{else}}<span class="pill bad">Live</span>{{end}}
    {{if .Killed}}<span class="pill bad">Kill switch on</span>{{else}}<span class="pill">Kill switch off</span>{{end}}
  </div>
</header>
<main>
  <section class="banner">
    <div>
      <strong>Tier 1 detects. Tier 2 only plans reversible runbooks. Tier 3 opens a pull request and stops.</strong>
      <div class="muted">Nothing in this console merges code. Dry run does not call Azure.</div>
    </div>
    <div class="row">
      {{if eq .Mode "demo"}}
      <form method="post" action="/v1/demo/scenario">
        <input type="hidden" name="token" value="{{.AdminToken}}">
        <button type="submit">Run example-api scenario</button>
      </form>
      {{end}}
      {{if .Killed}}
      <form method="post" action="/v1/control/resume">
        <input type="hidden" name="token" value="{{.AdminToken}}">
        <button class="quiet" type="submit">Release kill switch</button>
      </form>
      {{else}}
      <form method="post" action="/v1/control/kill">
        <input type="hidden" name="token" value="{{.AdminToken}}">
        <button class="quiet" type="submit">Engage kill switch</button>
      </form>
      {{end}}
    </div>
  </section>

  {{if eq .View "home"}}
  <table>
    <thead><tr><th>Severity</th><th>Incident</th><th>Count</th><th>Tier</th><th>Status</th><th>Seen</th></tr></thead>
    <tbody>
    {{if not .Incidents}}<tr><td colspan="6" class="muted">No open incidents. Run the example-api scenario to watch a null dereference move from diagnosis to a dry-run slot swap to a pull request.</td></tr>{{end}}
    {{range .Incidents}}
      <tr>
        <td class="sev sev-{{sevn .Severity}}">{{sev .Severity}}</td>
        <td><a href="/incidents/{{.ID}}">{{.Title}}</a><div class="muted">{{.ResourceName}}</div></td>
        <td>{{.Count}}</td>
        <td>{{tier .TierReached}}</td>
        <td>{{.Status}}</td>
        <td>{{when .LastSeen}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{end}}

  {{if eq .View "incident"}}
  <div class="grid">
    <div>
      <div class="card">
        <h2>{{.Incident.Title}}</h2>
        <p>{{.Incident.Message}}</p>
        <p class="muted">{{.Incident.ResourceID}}</p>
        <p>Status <strong>{{.Incident.Status}}</strong> · tier <strong>{{tier .Incident.TierReached}}</strong> · {{sev .Incident.Severity}} · count {{.Incident.Count}}</p>
        {{if .Incident.Hypothesis}}
          <p>{{.Incident.Hypothesis.Summary}}</p>
          <p class="muted">{{.Incident.Hypothesis.Provider}} · {{pct .Incident.Hypothesis.Confidence}} · {{.Incident.Hypothesis.Category}}
            {{if .Incident.Hypothesis.SuggestedAction}} · suggested {{.Incident.Hypothesis.SuggestedAction}}{{end}}</p>
          <ul>
          {{range .Incident.Hypothesis.Evidence}}<li>{{.}}</li>{{end}}
          </ul>
        {{end}}
        {{if .Incident.StackTrace}}<pre>{{.Incident.StackTrace}}</pre>{{end}}
      </div>
      <div class="card">
        <h2>Tier 2</h2>
        {{if not .Incident.Actions}}<p class="muted">No runbook was planned.</p>{{end}}
        {{range .Incident.Actions}}
          <p><strong>{{.Kind}}</strong> · {{.Outcome}}</p>
          <p class="muted">{{.Detail}}</p>
        {{end}}
      </div>
      <div class="card">
        <h2>Tier 3</h2>
        {{if .Incident.PullRequest}}
          <p><a href="{{.Incident.PullRequest.URL}}">{{.Incident.PullRequest.Title}}</a></p>
          <p class="muted">{{.Incident.PullRequest.Provider}} · {{.Incident.PullRequest.Branch}} · not merged</p>
        {{else}}<p class="muted">No pull request.</p>{{end}}
      </div>
    </div>
    <div>
      <div class="card">
        <h2>Audit</h2>
        {{range .Events}}
          <p><span class="muted">{{when .Time}}</span> {{.Kind}}<br>{{.Message}}</p>
        {{end}}
      </div>
      <form method="post" action="/v1/incidents/{{.Incident.ID}}/suppress">
        <input type="hidden" name="token" value="{{.AdminToken}}">
        <button class="quiet" type="submit">Suppress automation</button>
      </form>
    </div>
  </div>
  {{end}}

  {{if eq .View "audit"}}
  <table>
    <thead><tr><th>Time</th><th>Kind</th><th>Incident</th><th>Message</th></tr></thead>
    <tbody>
    {{range .Events}}
      <tr>
        <td>{{when .Time}}</td>
        <td>{{.Kind}}</td>
        <td>{{if .IncidentID}}<a href="/incidents/{{.IncidentID}}">{{.IncidentID}}</a>{{end}}</td>
        <td>{{.Message}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{end}}

  {{if eq .View "pull"}}
  <div class="card">
    <h2>{{.Pull.Ref.Title}}</h2>
    <p class="muted">{{.Pull.Ref.Provider}} · {{.Pull.Ref.Branch}} · Healbot does not merge this.</p>
    <pre>{{.Pull.Body}}</pre>
  </div>
  {{range .Pull.Files}}
  <div class="card">
    <h2>{{.Path}}</h2>
    <pre>{{.Content}}</pre>
  </div>
  {{end}}
  {{end}}
</main>
</body>
</html>
`
