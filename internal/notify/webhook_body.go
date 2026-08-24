package notify

// Body rendering and HMAC signing for the webhook channel.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"text/template"
)

// defaultWebhookBody is the default JSON payload sent to the webhook.
const defaultWebhookBody = `{"alert_id":"{{.AlertID}}","severity":"{{.Severity}}","state":"{{.State}}","check_id":"{{.CheckID}}","agent_id":"{{.AgentID}}","message":{{quote .Message}},"timestamp":"{{.Timestamp}}","platform_url":"{{.PlatformURL}}","alert_url":"{{.AlertURL}}"}`

// renderBody renders the body template against data, or returns the
// default JSON payload when no template is configured.
func renderBody(tplText string, data any) ([]byte, error) {
	if tplText == "" {
		tplText = defaultWebhookBody
	}
	funcs := template.FuncMap{
		"quote": func(s string) (string, error) {
			b, err := json.Marshal(s)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}
	tpl, err := template.New("body").Funcs(funcs).Parse(tplText)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// signHMAC returns the lowercase hex HMAC-SHA256 of msg using key.
func signHMAC(key string, msg []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(msg)
	return hex.EncodeToString(mac.Sum(nil))
}
