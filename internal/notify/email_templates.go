package notify

import (
	"html/template"
	"strings"
)

// emailHTMLTemplate is the HTML body. Uses {{ }} delimiters to avoid
// conflicts with Go template syntax elsewhere.
const emailHTMLTemplate = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; margin: 0; padding: 0; background: #f3f4f6; }
    .container { max-width: 600px; margin: 0 auto; background: #ffffff; }
    .header { padding: 20px 24px; color: #ffffff; font-size: 20px; font-weight: 600; }
    .body { padding: 24px; color: #1f2937; font-size: 14px; line-height: 1.5; }
    .body h2 { margin: 0 0 12px 0; font-size: 16px; }
    .body table { width: 100%; border-collapse: collapse; margin: 12px 0; }
    .body table td { padding: 8px 0; border-bottom: 1px solid #e5e7eb; font-size: 14px; }
    .body table td:first-child { font-weight: 600; width: 140px; color: #6b7280; }
    .message { background: #f9fafb; border-left: 3px solid #d1d5db; padding: 12px 16px; margin: 16px 0; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 13px; white-space: pre-wrap; }
    .footer { padding: 16px 24px; background: #f9fafb; font-size: 12px; color: #6b7280; text-align: center; }
    .btn { display: inline-block; background: #1f2937; color: #ffffff !important; text-decoration: none; padding: 10px 20px; border-radius: 4px; font-size: 14px; font-weight: 500; margin-top: 8px; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header" style="background: {{.Color}};">[{{.Severity | upper}}] {{.Title}}</div>
    <div class="body">
      <h2>Alert Details</h2>
      <table>
        <tr><td>Severity</td><td>{{.Severity}}</td></tr>
        <tr><td>Check</td><td>{{.CheckID}}</td></tr>
        <tr><td>Agent</td><td>{{.AgentID}}</td></tr>
        <tr><td>State</td><td>{{.State}}</td></tr>
        <tr><td>Triggered</td><td>{{.Timestamp}}</td></tr>
      </table>
      <div class="message">{{.Message}}</div>
      <a class="btn" href="{{.AlertURL}}">View Alert in OpenAgentPlatform</a>
    </div>
    <div class="footer">OpenAgentPlatform &middot; sent {{.SentAt}}</div>
  </div>
</body>
</html>`

// emailTextTemplate is the plain-text fallback.
const emailTextTemplate = `[{{.Severity | upper}}] {{.Title}}

Alert Details
-------------
Severity:   {{.Severity}}
Check:      {{.CheckID}}
Agent:      {{.AgentID}}
State:      {{.State}}
Triggered:  {{.Timestamp}}

{{.Message}}

View alert: {{.AlertURL}}
---

OpenAgentPlatform · sent {{.SentAt}}
`

var (
	htmlTpl = template.Must(template.New("email").Funcs(template.FuncMap{
		"upper": strings.ToUpper,
	}).Parse(emailHTMLTemplate))
	textTpl = template.Must(template.New("email_text").Funcs(template.FuncMap{
		"upper": strings.ToUpper,
	}).Parse(emailTextTemplate))
)
