// Package client is the dial side of the coordination plane: the connector a
// signed-in, opted-in local wallfacer instance runs to hold one long-lived
// outbound WebSocket to the coordinator. It shares the wire types with the
// accept side (the parent coordinator package) so the manifest and frame codec
// are defined once.
//
// See specs/cloud/latere-integration/coordination-plane/connection-and-presence/connection.md.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"latere.ai/x/pkg/retry"
	"latere.ai/x/pkg/wait"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"latere.ai/x/pkg/otel"

	"latere.ai/x/wallfacer/internal/coordinator"
)

// dialClient carries the WebSocket handshake. It is otel-instrumented so the
// upgrade request records a client span and sends traceparent, letting the
// coordinator's accept side continue the same trace instead of starting a new
// one. Its Timeout stays zero: coder/websocket rejects a client with a deadline
// because the connection outlives the request, so the dial budget comes from
// the context instead (dialTimeout).
var dialClient = otel.HTTPClient()

// ErrNotConnected is returned by Send when no live connection is held (signed
// out, opted out, or mid-reconnect). The caller surfaces it to the browser as a
// transient unavailability rather than a hard failure.
var ErrNotConnected = errors.New("coordination: not connected")

const (
	defaultPingInterval = 20 * time.Second
	defaultBaseBackoff  = 1 * time.Second
	defaultMaxBackoff   = 30 * time.Second
	dialTimeout         = 15 * time.Second
	// disabledRecheck is how often Run re-evaluates the opt-in gate while the
	// connector is idle (signed out or opted out).
	disabledRecheck = 2 * time.Second
	// enabledRecheck is how often a live connection re-checks the gate so an
	// opt-out or sign-out tears the socket down promptly.
	enabledRecheck = 1 * time.Second
)

// Config wires the connector to the host instance. The function fields are
// re-evaluated on each connect so a token refresh, an opt-in toggle, or a
// workspace switch is picked up on the next cycle without restarting Run.
type Config struct {
	// URL is the coordinator WebSocket endpoint (wss://wf.latere.ai/api/coordination/ws).
	URL string
	// Token returns the current bearer JWT and whether the instance is signed in.
	Token func() (string, bool)
	// OptedIn reports the coordination opt-in (the data-boundary gate; default off).
	OptedIn func() bool
	// Manifest builds the registration frame sent first on every connection.
	Manifest func() coordinator.Manifest
	// OnInbound, if set, receives the raw bytes of every coordinator-to-instance
	// text frame (presence snapshots, comment relay). The capability layer
	// decodes and dispatches it. Called from the read goroutine; it must not
	// block.
	OnInbound func([]byte)

	PingInterval time.Duration
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration

	Logger *slog.Logger
	// Rand returns a value in [0,1) for full-jitter backoff. Defaults to
	// math/rand; injectable for deterministic tests.
	Rand func() float64
}

// Connector holds one outbound connection to the coordinator, reconnecting with
// backoff while signed in and opted in.
type Connector struct {
	cfg Config

	// mu guards the live connection handle so Send (called from browser-facing
	// handlers) races safely with connect/teardown. writeMu serializes writes:
	// the WebSocket allows only one data writer at a time.
	mu      sync.Mutex
	conn    *websocket.Conn
	connCtx context.Context
	writeMu sync.Mutex

	// authRejected is set when the last dial was refused with 401/403 (a bad or
	// wrong-audience token), cleared on a successful dial. It lets the status
	// surface distinguish "the token is being rejected" from "still connecting",
	// so a hard auth misconfiguration is not hidden behind an endless spinner.
	authRejected atomic.Bool
}

// NewConnector applies defaults and returns a connector. Run drives it.
func NewConnector(cfg Config) *Connector {
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = defaultPingInterval
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = defaultBaseBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultMaxBackoff
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Float64
	}
	return &Connector{cfg: cfg}
}

// enabled is the opt-in gate: both signed in and opted in. A disabled connector
// dials nothing and emits nothing (the data-boundary guarantee).
func (c *Connector) enabled() bool {
	if c.cfg.OptedIn == nil || !c.cfg.OptedIn() {
		return false
	}
	if c.cfg.Token == nil {
		return false
	}
	_, ok := c.cfg.Token()
	return ok
}

// Run drives the connect/reconnect loop until ctx is cancelled. It is the only
// public entry point; call it in a goroutine from the instance's cloud-mode
// startup. While the gate is closed it idles; while open it holds a connection
// and reconnects with full-jitter exponential backoff on every drop.
func (c *Connector) Run(ctx context.Context) {
	schedule := retry.Policy{Base: c.cfg.BaseBackoff, Max: c.cfg.MaxBackoff, Jitter: -1}
	failures := 0
	for {
		if ctx.Err() != nil {
			return
		}
		if !c.enabled() {
			if wait.Sleep(ctx, disabledRecheck) != nil {
				return
			}
			continue
		}
		connected := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			failures = 0 // a real session dropped; retry promptly
		} else {
			failures++ // dial keeps failing; back off
		}
		if wait.Sleep(ctx, c.jitter(schedule.Delay(failures))) != nil {
			return
		}
	}
}

// connectOnce dials, registers the manifest, and pumps frames until the socket
// drops, the gate closes, or a ping goes unanswered. It returns true once a
// connection was actually established (used to reset backoff), false if the dial
// never succeeded.
func (c *Connector) connectOnce(ctx context.Context) bool {
	token, ok := c.cfg.Token()
	if !ok {
		return false
	}
	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	conn, resp, err := websocket.Dial(dctx, c.cfg.URL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		HTTPClient: dialClient,
	})
	dcancel()
	if err != nil {
		// A 401/403 is a persistent auth rejection (bad/expired token, wrong
		// audience): the socket will never come up until it is fixed, so log it
		// loudly and flag it rather than letting it churn silently at Debug behind
		// an eternal "connecting" status. Transient dial errors stay at Debug.
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			c.authRejected.Store(true)
			c.cfg.Logger.Warn("coordinator client: authentication rejected", "status", resp.StatusCode, "url", c.cfg.URL)
		} else {
			c.cfg.Logger.Debug("coordinator client: dial failed", "err", err)
		}
		return false
	}
	c.authRejected.Store(false)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "bye") }()

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.setConn(connCtx, conn)
	defer c.clearConn()

	if err := c.write(connCtx, conn, c.cfg.Manifest()); err != nil {
		c.cfg.Logger.Debug("coordinator client: manifest send failed", "err", err)
		return true
	}

	go c.pinger(connCtx, cancel, conn)
	go c.watchGate(connCtx, cancel)

	for {
		typ, data, err := conn.Read(connCtx)
		if err != nil {
			return true
		}
		// Coordinator-to-instance frames (comment relay, presence snapshots) are
		// handed to the capability layer via OnInbound; an unset callback drains
		// them, keeping the connection live (forward-compatible).
		if typ == websocket.MessageText && c.cfg.OnInbound != nil {
			c.cfg.OnInbound(data)
		}
	}
}

func (c *Connector) setConn(ctx context.Context, conn *websocket.Conn) {
	c.mu.Lock()
	c.conn, c.connCtx = conn, ctx
	c.mu.Unlock()
}

func (c *Connector) clearConn() {
	c.mu.Lock()
	c.conn, c.connCtx = nil, nil
	c.mu.Unlock()
}

// Connected reports whether a live connection to the coordinator is currently
// held. Used to surface the coordination state in the UI.
func (c *Connector) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// AuthRejected reports whether the most recent dial was refused with 401/403,
// i.e. the token is being rejected (bad, expired, or wrong audience) rather than
// the connection merely still being established. Cleared on the next successful
// dial. Used to surface a hard auth misconfiguration in the coordination status.
func (c *Connector) AuthRejected() bool {
	return c.authRejected.Load()
}

// Send writes a frame to the live coordinator connection. It is safe for
// concurrent use (browser-facing handlers call it). Returns ErrNotConnected
// when no connection is held; the caller treats that as transient.
func (c *Connector) Send(v any) error {
	c.mu.Lock()
	conn, connCtx := c.conn, c.connCtx
	c.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	return c.write(connCtx, conn, v)
}

// write serializes a single data-frame write (the WebSocket allows one writer).
func (c *Connector) write(ctx context.Context, conn *websocket.Conn, v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeJSON(ctx, conn, v)
}

// pinger sends a ping every PingInterval and tears the connection down if one
// goes unanswered within three intervals (the 60 s liveness window).
func (c *Connector) pinger(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	t := time.NewTicker(c.cfg.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, 3*c.cfg.PingInterval)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

// watchGate cancels the connection when the opt-in gate closes (sign-out or
// opt-out), giving a clean, prompt teardown rather than waiting for a drop.
func (c *Connector) watchGate(ctx context.Context, cancel context.CancelFunc) {
	t := time.NewTicker(enabledRecheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !c.enabled() {
				cancel()
				return
			}
		}
	}
}

// jitter draws a uniform random duration in [0, d] (full jitter). It stays
// local rather than using retry.Policy.Jitter because the random source is
// a Config seam that tests pin to exact values.
func (c *Connector) jitter(d time.Duration) time.Duration {
	return time.Duration(c.cfg.Rand() * float64(d))
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}
