package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/oidc"

	"golang.org/x/oauth2"
)

// DeviceAuth drives the local SPA's RFC 8628 device-code sign-in via three
// endpoints:
//
//	POST /api/auth/device/start  -> { user_code, verification_uri, ... }
//	GET  /api/auth/device/poll   -> { status: pending | done | denied, ... }
//	POST /api/auth/device/cancel -> {}
//
// Token persistence is done via authkit.FileTokenStore at
// <UserConfigDir>/latere/token.json, the same path latere-cli and the
// `wallfacer auth login` CLI use, so all three share a single login.
//
// One concurrent device flow at a time; an in-flight flow is overwritten
// when /start is called again (the previous flow is cancelled).
type DeviceAuth struct {
	OIDC      *oidc.Client
	Store     authkit.TokenStore
	NewClient func() *oidc.Client // optional override for tests

	mu   sync.Mutex
	flow *deviceFlowState
}

type deviceFlowState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	expiresAt time.Time

	// Populated by the device-auth call.
	verificationURI string
	userCode        string

	// Populated when the poll loop finishes.
	finalErr error
}

// Mount registers the device-auth routes on mux. Safe to call when DeviceAuth
// is nil; the endpoints answer 503 in that case so the SPA can detect "auth
// not available" without distinguishing 404 / 503.
func (d *DeviceAuth) Mount(mux *http.ServeMux) {
	if d == nil {
		mux.HandleFunc("POST /api/auth/device/start", deviceUnavailable)
		mux.HandleFunc("GET /api/auth/device/poll", deviceUnavailable)
		mux.HandleFunc("POST /api/auth/device/cancel", deviceUnavailable)
		return
	}
	mux.HandleFunc("POST /api/auth/device/start", d.start)
	mux.HandleFunc("GET /api/auth/device/poll", d.poll)
	mux.HandleFunc("POST /api/auth/device/cancel", d.cancel)
}

// AuthDeviceStart is exposed as a *Handler method so the apicontract-driven
// route registry in internal/cli/server.go can map it like any other endpoint.
// AuthDevicePoll and AuthDeviceCancel below mirror the same pattern.
func (h *Handler) AuthDeviceStart(w http.ResponseWriter, r *http.Request) {
	if h.deviceAuth == nil {
		deviceUnavailable(w, r)
		return
	}
	h.deviceAuth.start(w, r)
}

// AuthDevicePoll proxies to the device-code driver; see AuthDeviceStart.
func (h *Handler) AuthDevicePoll(w http.ResponseWriter, r *http.Request) {
	if h.deviceAuth == nil {
		deviceUnavailable(w, r)
		return
	}
	h.deviceAuth.poll(w, r)
}

// AuthDeviceCancel proxies to the device-code driver; see AuthDeviceStart.
func (h *Handler) AuthDeviceCancel(w http.ResponseWriter, r *http.Request) {
	if h.deviceAuth == nil {
		deviceUnavailable(w, r)
		return
	}
	h.deviceAuth.cancel(w, r)
}

func deviceUnavailable(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "device-code auth not configured", http.StatusServiceUnavailable)
}

type startRequest struct {
	OrgID    string `json:"org_id,omitempty"`
	Personal bool   `json:"personal,omitempty"`
}

type startResponse struct {
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	UserCode                string `json:"user_code"`
	ExpiresIn               int    `json:"expires_in"`
}

func (d *DeviceAuth) start(w http.ResponseWriter, r *http.Request) {
	body, ok := httpjson.DecodeOptionalBody[startRequest](w, r)
	if !ok {
		return
	}

	d.mu.Lock()
	if d.flow != nil {
		// Cancel the in-flight flow before starting a new one.
		d.flow.cancel()
		d.flow = nil
	}
	d.mu.Unlock()

	client := d.OIDC
	if d.NewClient != nil {
		client = d.NewClient()
	}
	if client == nil {
		http.Error(w, "device-code auth not configured", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	extra := url.Values{}
	if body.Personal || body.OrgID != "" {
		extra["org_id"] = []string{body.OrgID}
	}

	da, err := client.DeviceAuth(ctx, extra)
	if err != nil {
		cancel()
		http.Error(w, fmt.Sprintf("device authorization: %v", err), http.StatusBadGateway)
		return
	}

	verify := da.VerificationURIComplete
	if verify == "" {
		verify = da.VerificationURI
	}

	flow := &deviceFlowState{
		ctx:             ctx,
		cancel:          cancel,
		done:            make(chan struct{}),
		expiresAt:       time.Now().Add(time.Duration(time.Until(da.Expiry).Seconds()) * time.Second),
		verificationURI: verify,
		userCode:        da.UserCode,
	}
	if da.Expiry.IsZero() {
		// oauth2 sometimes omits Expiry; fall back to ExpiresIn semantics.
		flow.expiresAt = time.Now().Add(15 * time.Minute)
	}

	d.mu.Lock()
	d.flow = flow
	d.mu.Unlock()

	go func() {
		defer close(flow.done)
		tok, perr := client.DeviceAccessToken(flow.ctx, da)
		if perr != nil {
			d.mu.Lock()
			flow.finalErr = perr
			d.mu.Unlock()
			return
		}
		if perr := d.Store.Save(tok); perr != nil {
			d.mu.Lock()
			flow.finalErr = perr
			d.mu.Unlock()
			return
		}
	}()

	expiresIn := int(time.Until(flow.expiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}
	httpjson.Write(w, http.StatusOK, startResponse{
		VerificationURI:         da.VerificationURI,
		VerificationURIComplete: verify,
		UserCode:                da.UserCode,
		ExpiresIn:               expiresIn,
	})
}

type pollResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (d *DeviceAuth) poll(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	flow := d.flow
	d.mu.Unlock()

	if flow == nil {
		httpjson.Write(w, http.StatusOK, pollResponse{Status: "idle"})
		return
	}
	select {
	case <-flow.done:
		d.mu.Lock()
		err := flow.finalErr
		// Clear the flow now that it's terminal.
		d.flow = nil
		d.mu.Unlock()
		if err != nil {
			status := "denied"
			var rerr *oauth2.RetrieveError
			if errors.As(err, &rerr) {
				switch rerr.ErrorCode {
				case "expired_token":
					status = "expired"
				case "access_denied":
					status = "denied"
				}
			}
			httpjson.Write(w, http.StatusOK, pollResponse{Status: status, Error: err.Error()})
			return
		}
		httpjson.Write(w, http.StatusOK, pollResponse{Status: "done"})
	default:
		httpjson.Write(w, http.StatusOK, pollResponse{Status: "pending"})
	}
}

func (d *DeviceAuth) cancel(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	if d.flow != nil {
		d.flow.cancel()
		d.flow = nil
	}
	d.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
