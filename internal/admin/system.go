package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// systemProbeTimeout caps how long `claude --version` and `claude auth status`
// may run. The endpoint stays useful even when Claude Code is missing or
// slow to start, so a short ceiling is fine.
const systemProbeTimeout = 3 * time.Second

// systemStatus is the payload returned to the UI. Fields are populated
// best-effort; the handler never fails the request when Claude Code is
// missing — `error` reports the reason instead.
type systemStatus struct {
	ClaudeBinary      string `json:"claude_binary"`
	ClaudeBinaryFound bool   `json:"claude_binary_found"`
	ClaudeVersion     string `json:"claude_version,omitempty"`
	AuthMethod        string `json:"auth_method"` // "oauth_token" / "claudeai_login" / "api_key" / "none" / "unknown"
	AuthAccount       string `json:"auth_account,omitempty"`
	AuthOrg           string `json:"auth_org,omitempty"`
	AuthOrgID         string `json:"auth_org_id,omitempty"`
	AuthSubscription  string `json:"auth_subscription,omitempty"`
	AuthExpiresAt     string `json:"auth_expires_at,omitempty"`
	ConfigDirectory   string `json:"config_directory,omitempty"`
	Notes             string `json:"notes,omitempty"`
}

// getSystemStatus runs the small probes that back the Settings page's
// "Claude Code" block and returns one JSON response. It does not write
// anything; everything is read-only against the running binary and the
// CLAUDE_CONFIG_DIR the user already configured.
func (a *API) getSystemStatus(w http.ResponseWriter, r *http.Request) {
	status := systemStatus{
		AuthMethod: "unknown",
	}
	if a.claude != "" {
		status.ClaudeBinary = a.claude
		if _, err := exec.LookPath(a.claude); err == nil {
			status.ClaudeBinaryFound = true
		}
	}
	if status.ClaudeBinaryFound {
		if out, err := runShort(r.Context(), a.claude, "--version"); err == nil {
			status.ClaudeVersion = strings.TrimSpace(out)
		}
		if out, err := runShort(r.Context(), a.claude, "auth", "status"); err == nil {
			applyAuthStatus(out, &status)
		}
	} else if status.ClaudeBinary != "" {
		status.Notes = "`claude` not on PATH inside this container; check the image build and the CLAUDE_CONFIG_DIR mount."
	}
	if status.AuthMethod == "unknown" {
		switch {
		case envHas(a.envs, "CLAUDE_CODE_OAUTH_TOKEN"):
			status.AuthMethod = "oauth_token"
			status.Notes = "CLAUDE_CODE_OAUTH_TOKEN is set; `claude auth status` could not confirm."
		case envHas(a.envs, "ANTHROPIC_API_KEY"):
			status.AuthMethod = "api_key"
			status.Notes = "ANTHROPIC_API_KEY is set; Claude Code login is not used."
		default:
			status.AuthMethod = "none"
			status.Notes = "Set CLAUDE_CODE_OAUTH_TOKEN or exec `claude auth login` in this container."
		}
	}
	if cd := envValue(a.envs, "CLAUDE_CONFIG_DIR"); cd != "" {
		status.ConfigDirectory = cd
	}
	writeJSON(w, http.StatusOK, status)
}

// runShort runs a `claude` command with the short probe timeout. The error
// carries stderr; the handler swallows it.
func runShort(parent context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, systemProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// authStatus is the JSON shape Claude Code 2.1+ prints for `auth status`.
// Some fields are absent in earlier versions, so they are pointers.
type authStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	Email            string `json:"email"`
	OrgName          string `json:"orgName"`
	OrgID            string `json:"orgId"`
	SubscriptionType string `json:"subscriptionType"`
	ConfigDirectory  string `json:"configDirectory"`
}

// applyAuthStatus populates status from the JSON payload of
// `claude auth status`. Older versions printed a short human-readable text;
// fall back to the loose-string parser so the endpoint still works there.
func applyAuthStatus(out string, status *systemStatus) {
	trimmed := strings.TrimSpace(out)
	var s authStatus
	if !strings.HasPrefix(trimmed, "{") || json.Unmarshal([]byte(trimmed), &s) != nil {
		parseAuthText(out, status)
		return
	}
	if !s.LoggedIn {
		status.AuthMethod = "none"
		status.Notes = "`claude` is installed but not authenticated inside this container."
		return
	}
	switch strings.ToLower(s.AuthMethod) {
	case "claude.ai", "claudeai", "subscription", "":
		status.AuthMethod = "claudeai_login"
	case "console", "api_key":
		status.AuthMethod = "api_key"
	case "oauth_token":
		status.AuthMethod = "oauth_token"
	default:
		status.AuthMethod = "claudeai_login"
	}
	status.AuthAccount = s.Email
	status.AuthOrg = s.OrgName
	status.AuthOrgID = s.OrgID
	status.AuthSubscription = s.SubscriptionType
	if status.ConfigDirectory == "" && s.ConfigDirectory != "" {
		status.ConfigDirectory = s.ConfigDirectory
	}
}

// parseAuthText keeps the loose-text behavior as a fallback for older
// Claude Code versions that did not print JSON.
func parseAuthText(out string, status *systemStatus) {
	lower := strings.ToLower(out)
	// "not logged in" contains "logged in", so it has to be checked first.
	if strings.Contains(lower, "not logged in") {
		status.AuthMethod = "none"
		status.Notes = "`claude` is installed but not authenticated inside this container."
		return
	}
	if !strings.Contains(lower, "logged in") {
		return
	}
	status.AuthMethod = "claudeai_login"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch strings.ToLower(key) {
		case "email", "account":
			status.AuthAccount = val
		case "organization", "org":
			status.AuthOrg = val
		case "subscription":
			status.AuthSubscription = val
		case "token expires", "expires":
			status.AuthExpiresAt = val
		}
	}
}

func envHas(envs []string, key string) bool {
	return envValue(envs, key) != ""
}

func envValue(envs []string, key string) string {
	prefix := key + "="
	for _, kv := range envs {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}
