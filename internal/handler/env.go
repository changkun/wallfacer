package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
)

// privateIPNets lists networks blocked for SSRF prevention: RFC 1918 private
// ranges, loopback (IPv4 and IPv6), and link-local ranges.
var privateIPNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"169.254.0.0/16",
		"fe80::/10",
	} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid CIDR in privateIPNets: " + cidr)
		}
		privateIPNets = append(privateIPNets, network)
	}
}

// isPrivateIP reports whether ip falls in a private, loopback, or link-local
// address range.
func isPrivateIP(ip net.IP) bool {
	for _, network := range privateIPNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// validateBaseURL validates that u is safe to use as a remote API base URL.
// It rejects: non-https schemes, bare IP addresses, single-label hostnames
// (e.g. "localhost"), and hostnames that resolve to private/loopback/link-local
// IP addresses.
func validateBaseURL(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be https, got %q", parsed.Scheme)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a non-empty hostname")
	}
	// Reject bare IP addresses (IPv4 and IPv6).
	if ip := net.ParseIP(hostname); ip != nil {
		return fmt.Errorf("bare IP addresses are not allowed as the base URL hostname")
	}
	// Require at least one dot to rule out single-label names like "localhost"
	// or internal container names that may resolve to private addresses.
	if !strings.Contains(hostname, ".") {
		return fmt.Errorf("hostname %q must be a fully qualified domain name (must contain at least one dot)", hostname)
	}
	// Resolve to IPs and verify none fall in a blocked range.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", hostname, err)
	}
	for _, addr := range addrs {
		if isPrivateIP(addr.IP) {
			return fmt.Errorf("hostname %q resolves to a restricted IP address (%s)", hostname, addr.IP)
		}
	}
	return nil
}

// envConfigResponse is the JSON representation of the env config sent to the UI.
// Sensitive tokens are masked so they are never exposed in full over HTTP.
type envConfigResponse struct {
	OAuthToken        string `json:"oauth_token"`        // masked
	APIKey            string `json:"api_key"`             // masked
	BaseURL           string `json:"base_url"`
	DefaultModel      string `json:"default_model"`
	TitleModel        string `json:"title_model"`
	MaxParallelTasks  int    `json:"max_parallel_tasks"`
	OversightInterval int    `json:"oversight_interval"`
}

// GetEnvConfig returns the current env configuration with tokens masked.
func (h *Handler) GetEnvConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		http.Error(w, "failed to read env file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	maxParallel := cfg.MaxParallelTasks
	if maxParallel <= 0 {
		maxParallel = defaultMaxConcurrentTasks
	}
	writeJSON(w, http.StatusOK, envConfigResponse{
		OAuthToken:        envconfig.MaskToken(cfg.OAuthToken),
		APIKey:            envconfig.MaskToken(cfg.APIKey),
		BaseURL:           cfg.BaseURL,
		DefaultModel:      cfg.DefaultModel,
		TitleModel:        cfg.TitleModel,
		MaxParallelTasks:  maxParallel,
		OversightInterval: cfg.OversightInterval,
	})
}

// UpdateEnvConfig writes changes to the env file.
//
// Pointer semantics per field:
//   - field absent from JSON body (null) → leave unchanged
//   - field present with a value          → update
//   - field present with ""               → clear (for non-secret fields)
//
// For the two token fields (oauth_token, api_key), an empty value is treated
// as "no change" to prevent accidental token deletion.
func (h *Handler) UpdateEnvConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OAuthToken        *string `json:"oauth_token"`
		APIKey            *string `json:"api_key"`
		BaseURL           *string `json:"base_url"`
		DefaultModel      *string `json:"default_model"`
		TitleModel        *string `json:"title_model"`
		MaxParallelTasks  *int    `json:"max_parallel_tasks"`
		OversightInterval *int    `json:"oversight_interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Guard: treat empty-string tokens as "no change" to avoid accidental clears.
	if req.OAuthToken != nil && *req.OAuthToken == "" {
		req.OAuthToken = nil
	}
	if req.APIKey != nil && *req.APIKey == "" {
		req.APIKey = nil
	}

	// Convert max_parallel_tasks int to string for the env file.
	var maxParallel *string
	if req.MaxParallelTasks != nil {
		v := *req.MaxParallelTasks
		if v < 1 {
			v = 1
		}
		s := fmt.Sprintf("%d", v)
		maxParallel = &s
	}

	// Convert oversight_interval int to string for the env file.
	// Clamp to [0, 120]: 0 = disabled; 120 minutes = max.
	var oversightInterval *string
	if req.OversightInterval != nil {
		v := *req.OversightInterval
		if v < 0 {
			v = 0
		}
		if v > 120 {
			v = 120
		}
		s := fmt.Sprintf("%d", v)
		oversightInterval = &s
	}

	// Validate the base URL if provided to prevent SSRF.
	if req.BaseURL != nil && *req.BaseURL != "" {
		if err := validateBaseURL(*req.BaseURL); err != nil {
			http.Error(w, "invalid base_url: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	if err := envconfig.Update(h.envFile, req.OAuthToken, req.APIKey, req.BaseURL, req.DefaultModel, req.TitleModel, maxParallel, oversightInterval); err != nil {
		http.Error(w, "failed to update env file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// When the parallel task limit changes, re-evaluate immediately so new
	// capacity is filled without waiting for the next store event.
	if req.MaxParallelTasks != nil {
		go h.tryAutoPromote(context.Background())
	}

	w.WriteHeader(http.StatusNoContent)
}
