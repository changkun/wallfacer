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
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"latere.ai/x/wallfacer/internal/coordinator"
)

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
	backoff := c.cfg.BaseBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		if !c.enabled() {
			if !sleepCtx(ctx, disabledRecheck) {
				return
			}
			continue
		}
		connected := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = c.cfg.BaseBackoff // a real session dropped; retry promptly
		}
		if !sleepCtx(ctx, c.jitter(backoff)) {
			return
		}
		if !connected {
			backoff = nextBackoff(backoff, c.cfg.MaxBackoff) // dial keeps failing; back off
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
	conn, _, err := websocket.Dial(dctx, c.cfg.URL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	dcancel()
	if err != nil {
		c.cfg.Logger.Debug("coordinator client: dial failed", "err", err)
		return false
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "bye") }()

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := writeJSON(connCtx, conn, c.cfg.Manifest()); err != nil {
		c.cfg.Logger.Debug("coordinator client: manifest send failed", "err", err)
		return true
	}

	go c.pinger(connCtx, cancel, conn)
	go c.watchGate(connCtx, cancel)

	for {
		if _, _, err := conn.Read(connCtx); err != nil {
			return true
		}
		// Coordinator-to-instance frames (presence snapshots, commands, comment
		// relay) are consumed by later capability leaves; this leaf only keeps
		// the connection live and drains them.
	}
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

func (c *Connector) jitter(d time.Duration) time.Duration {
	// Full jitter: a uniform random duration in [0, d].
	return time.Duration(c.cfg.Rand() * float64(d))
}

// nextBackoff doubles cur, capped at limit.
func nextBackoff(cur, limit time.Duration) time.Duration {
	n := cur * 2
	if n > limit {
		return limit
	}
	return n
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

// sleepCtx sleeps for d, returning false if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
