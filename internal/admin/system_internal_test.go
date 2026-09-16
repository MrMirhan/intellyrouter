package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyAuthStatusJSON(t *testing.T) {
	// The shape Claude Code 2.1 prints for `claude auth status`.
	out := `{
	  "loggedIn": true,
	  "authMethod": "claude.ai",
	  "apiProvider": "firstParty",
	  "configDirectory": "/data/.claude",
	  "email": "dev@example.com",
	  "orgId": "org-123",
	  "orgName": "Example Org",
	  "subscriptionType": "max"
	}`
	var got systemStatus
	applyAuthStatus(out, &got)
	if got.AuthMethod != "claudeai_login" || got.AuthAccount != "dev@example.com" ||
		got.AuthOrg != "Example Org" || got.AuthOrgID != "org-123" ||
		got.AuthSubscription != "max" || got.ConfigDirectory != "/data/.claude" {
		t.Fatalf("status = %+v", got)
	}

	// A logged-out payload is still JSON, so it must not fall through to the
	// text parser and leave the method unknown.
	got = systemStatus{AuthMethod: "unknown"}
	applyAuthStatus(`{"loggedIn": false}`, &got)
	if got.AuthMethod != "none" || got.Notes == "" {
		t.Fatalf("logged out: %+v", got)
	}

	// An API-key login reports its own method.
	got = systemStatus{}
	applyAuthStatus(`{"loggedIn": true, "authMethod": "console"}`, &got)
	if got.AuthMethod != "api_key" {
		t.Fatalf("console login: %+v", got)
	}
}

func TestApplyAuthStatusText(t *testing.T) {
	// Older versions printed lines instead of JSON.
	var got systemStatus
	applyAuthStatus("Logged in as dev@example.com\nEmail: dev@example.com\nOrganization: Example Org\nExpires: 2027-01-01", &got)
	if got.AuthMethod != "claudeai_login" || got.AuthAccount != "dev@example.com" ||
		got.AuthOrg != "Example Org" || got.AuthExpiresAt != "2027-01-01" {
		t.Fatalf("status = %+v", got)
	}

	got = systemStatus{AuthMethod: "unknown"}
	applyAuthStatus("Not logged in", &got)
	if got.AuthMethod != "none" {
		t.Fatalf("logged out: %+v", got)
	}
}

func TestSystemStatusMissingBinary(t *testing.T) {
	a := &API{claude: "intellyrouter-no-such-binary", envs: []string{"CLAUDE_CONFIG_DIR=/data/.claude"}}
	w := httptest.NewRecorder()
	a.getSystemStatus(w, httptest.NewRequest("GET", "/api/admin/system/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"claude_binary_found":false`, `"auth_method":"none"`, `"config_directory":"/data/.claude"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestSystemStatusFallsBackToEnvCredentials(t *testing.T) {
	a := &API{claude: "intellyrouter-no-such-binary", envs: []string{"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-x"}}
	w := httptest.NewRecorder()
	a.getSystemStatus(w, httptest.NewRequest("GET", "/api/admin/system/status", nil))
	body := w.Body.String()
	if !strings.Contains(body, `"auth_method":"oauth_token"`) {
		t.Fatalf("body = %s", body)
	}
	// The token value itself must never reach the response.
	if strings.Contains(body, "sk-ant-oat01-x") {
		t.Fatalf("response leaked the token: %s", body)
	}
}
