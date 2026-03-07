package handler

import "testing"

func TestBuildMessagesURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		apiKey      string
		authToken   string
		oauthToken  string
		wantURL     string
	}{
		{
			name:    "api key only uses api.anthropic.com/v1/messages",
			apiKey:  "sk-ant-test",
			wantURL: "https://api.anthropic.com/v1/messages",
		},
		{
			name:       "oauth token only uses api.claude.ai/api/messages",
			oauthToken: "oauth-token",
			wantURL:    "https://api.claude.ai/api/messages",
		},
		{
			name:      "gateway auth token uses api.anthropic.com/v1/messages",
			authToken: "gateway-token",
			wantURL:   "https://api.anthropic.com/v1/messages",
		},
		{
			name:       "explicit base URL takes precedence over oauth detection",
			baseURL:    "https://my-gateway.example.com",
			oauthToken: "oauth-token",
			wantURL:    "https://my-gateway.example.com/v1/messages",
		},
		{
			name:    "explicit base URL trailing slash is trimmed",
			baseURL: "https://my-gateway.example.com/",
			apiKey:  "sk-ant-test",
			wantURL: "https://my-gateway.example.com/v1/messages",
		},
		{
			name:       "api key wins over oauth when both set (api key takes priority)",
			apiKey:     "sk-ant-test",
			oauthToken: "oauth-token",
			wantURL:    "https://api.anthropic.com/v1/messages",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildMessagesURL(tc.baseURL, tc.apiKey, tc.authToken, tc.oauthToken)
			if got != tc.wantURL {
				t.Errorf("buildMessagesURL(%q, %q, %q, %q) = %q; want %q",
					tc.baseURL, tc.apiKey, tc.authToken, tc.oauthToken, got, tc.wantURL)
			}
		})
	}
}
