package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"latere.ai/x/wallfacer/internal/envconfig"
)

func TestEnvKeyringSettingsAndTestIsolation(t *testing.T) {
	keyring.MockInit()
	h, path := newTestHandlerWithEnv(t)
	h.envFile = path
	put := httptest.NewRecorder()
	h.UpdateEnvConfig(put, httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"secret_store":"keyring","api_key":"provider-secret-value"}`)))
	if put.Code != 204 {
		t.Fatalf("save: %d %s", put.Code, put.Body.String())
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "provider-secret-value") {
		t.Fatal("API left provider credential in plaintext")
	}
	get := httptest.NewRecorder()
	h.GetEnvConfig(get, httptest.NewRequest(http.MethodGet, "/api/env", nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"secret_store":"keyring"`) || strings.Contains(get.Body.String(), "provider-secret-value") {
		t.Fatalf("get did not return masked keyring config: %d", get.Code)
	}
	token := "temporary-secret"
	temporary, err := h.buildTestEnvFile(&sandboxTestRequest{APIKey: &token})
	if err != nil {
		t.Fatal(err)
	}
	defer envconfig.RemoveTestFile(temporary)
	cfg, err := envconfig.Parse(temporary)
	if err != nil || cfg.APIKey != token {
		t.Fatal("test credentials not applied")
	}
	cfg, err = envconfig.Parse(path)
	if err != nil || cfg.APIKey != "provider-secret-value" {
		t.Fatal("test modified saved credentials")
	}
	if err := newOAuthTokenWriter(path)("CLAUDE_CODE_OAUTH_TOKEN", "oauth-secret"); err != nil {
		t.Fatal(err)
	}
	cfg, err = envconfig.Parse(path)
	if err != nil || cfg.OAuthToken != "oauth-secret" || cfg.APIKey != "provider-secret-value" {
		t.Fatal("OAuth did not preserve provider storage")
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "oauth-secret") {
		t.Fatal("OAuth wrote plaintext")
	}
	keyring.MockInitWithError(errors.New("locked"))
	failed := httptest.NewRecorder()
	h.GetEnvConfig(failed, httptest.NewRequest(http.MethodGet, "/api/env", nil))
	if failed.Code != 500 {
		t.Fatal("locked keyring should be a visible failure")
	}
}

func TestEnvRejectsUnknownSecretStore(t *testing.T) {
	h, path := newTestHandlerWithEnv(t)
	h.envFile = path
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"secret_store":"invalid"}`)))
	if w.Code != 422 {
		t.Fatalf("status = %d", w.Code)
	}
}
