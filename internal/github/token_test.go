package github

import (
	"testing"
	"time"
)

func TestToken_Expired(t *testing.T) {
	tests := []struct {
		name string
		tok  *Token
		want bool
	}{
		{"nil token", nil, true},
		{"zero expiry treated as live", &Token{AccessToken: "x"}, false},
		{"future expiry", &Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}, false},
		{"past expiry", &Token{AccessToken: "x", Expiry: time.Now().Add(-time.Hour)}, true},
		{"within leeway counts as expired", &Token{AccessToken: "x", Expiry: time.Now().Add(expiryLeeway / 2)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tok.Expired(); got != tt.want {
				t.Errorf("Expired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToken_Valid(t *testing.T) {
	tests := []struct {
		name string
		tok  *Token
		want bool
	}{
		{"nil", nil, false},
		{"empty access token", &Token{Expiry: time.Now().Add(time.Hour)}, false},
		{"expired", &Token{AccessToken: "x", Expiry: time.Now().Add(-time.Minute)}, false},
		{"usable", &Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}, true},
		{"usable no expiry", &Token{AccessToken: "x"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tok.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}
