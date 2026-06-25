package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/speccomment"
)

// Timing for the accept-side connection. The client pings every pingInterval;
// the coordinator drops a peer that fails to answer a ping within
// livenessTimeout (the parent's 60 s liveness window). A connection that does
// not send its manifest within handshakeTimeout is closed.
const (
	handshakeTimeout = 10 * time.Second
	pingInterval     = 20 * time.Second
	livenessTimeout  = 60 * time.Second
)

// Coordinator is the accept side of the coordination plane: the wallfacerd role
// that terminates the outbound WebSocket each signed-in, opted-in local instance
// dials. It validates the principal, registers the instance's manifest, and
// keeps the connection live; capability code (presence, projection, remote
// control, comments) reads the registry it feeds. It holds no task or content
// data.
type Coordinator struct {
	reg      *Registry
	log      *slog.Logger
	comments *CommentService // nil until SetCommentService; the spec-comment capability
}

// NewCoordinator returns a Coordinator feeding the given registry.
func NewCoordinator(reg *Registry) *Coordinator {
	return &Coordinator{reg: reg, log: slog.Default()}
}

// SetCommentService attaches the spec-comment capability so the coordinator
// dispatches FrameSpecComment ops and syncs threads on connect. Without it,
// comment frames are ignored (forward-compatible).
func (c *Coordinator) SetCommentService(s *CommentService) { c.comments = s }

// HandleWS is the GET /api/coordination/ws handler. It must run behind the
// server's auth path: the principal is read from the validated request context
// (never from the manifest body), so a failed validation never reaches here.
func (c *Coordinator) HandleWS(w http.ResponseWriter, r *http.Request) {
	ident, ok := auth.PrincipalFromContext(r.Context())
	if !ok || ident == nil || ident.Sub == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	principal := Principal{Sub: ident.Sub, OrgID: ident.OrgID}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The dialer is a programmatic wallfacer instance authenticated by its
		// bearer JWT, not a browser, so there is no Origin to check (this is not
		// a CSRF surface). Auth is the JWT validated upstream of this handler.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	connCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c.serve(connCtx, cancel, conn, principal)
}

// serve runs one connection: read the manifest, register, then pump frames
// until the socket closes or the peer goes silent.
func (c *Coordinator) serve(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, p Principal) {
	m, err := c.readManifest(ctx, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "manifest required")
		return
	}

	inst := Instance{Principal: p, Manifest: m, Conn: &wsSender{conn: conn, ctx: ctx}}
	reg := c.reg.Join(inst)
	defer c.reg.LeaveRegistration(reg)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "bye") }()

	go c.pinger(ctx, cancel, conn)

	// Push the comment threads for the repos this instance serves, so a freshly
	// connected board has its comments without a separate fetch.
	if c.comments != nil {
		c.comments.SyncTo(ctx, inst)
	}

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		c.dispatch(ctx, p, m.InstanceID, data)
	}
}

// readManifest reads and validates the first frame as a manifest. The instance
// id must be present; principal and org are intentionally absent from the body
// (the coordinator derives them from the JWT).
func (c *Coordinator) readManifest(ctx context.Context, conn *websocket.Conn) (Manifest, error) {
	hctx, hcancel := context.WithTimeout(ctx, handshakeTimeout)
	defer hcancel()
	typ, data, err := conn.Read(hctx)
	if err != nil {
		return Manifest{}, err
	}
	if typ != websocket.MessageText {
		return Manifest{}, errors.New("manifest must be a text frame")
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		return Manifest{}, err
	}
	if env.Type != FrameManifest {
		return Manifest{}, fmt.Errorf("first frame must be %q, got %q", FrameManifest, env.Type)
	}
	var m Manifest
	if err := json.Unmarshal(env.Raw, &m); err != nil {
		return Manifest{}, err
	}
	if m.InstanceID == "" {
		return Manifest{}, errors.New("manifest missing instance_id")
	}
	return m, nil
}

// dispatch handles a post-handshake frame. A manifest re-registers the instance
// (reconnect or workspace-set change), but only for the same instance id this
// socket opened with. Reserved capability frames and unknown types are ignored
// without dropping the connection (forward-compatible with newer peers).
func (c *Coordinator) dispatch(ctx context.Context, p Principal, instanceID string, data []byte) {
	env, err := DecodeEnvelope(data)
	if err != nil {
		c.log.Warn("coordinator: bad frame", "err", err)
		return
	}
	switch env.Type {
	case FrameManifest:
		var m Manifest
		if err := json.Unmarshal(env.Raw, &m); err != nil {
			c.log.Warn("coordinator: bad manifest update", "err", err)
			return
		}
		if m.InstanceID != instanceID {
			c.log.Warn("coordinator: manifest instance_id mismatch on live socket",
				"want", instanceID, "got", m.InstanceID)
			return
		}
		c.reg.UpdateManifest(instanceID, m)
		// A workspace-set change may newly serve a repo; re-sync its comments.
		if c.comments != nil {
			if inst, ok := c.reg.instance(instanceID); ok {
				c.comments.SyncTo(ctx, inst)
			}
		}
	case FrameSpecComment:
		c.dispatchComment(ctx, p, env.Raw)
	default:
		c.log.Debug("coordinator: ignoring frame type", "type", env.Type)
	}
}

// dispatchComment decodes a spec-comment op and applies it through the comment
// capability, which mints ids, stamps the principal, persists, and fans out. The
// principal is the validated connection identity, never the wire body.
func (c *Coordinator) dispatchComment(ctx context.Context, p Principal, raw []byte) {
	if c.comments == nil {
		return
	}
	var ev speccomment.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		c.log.Warn("coordinator: bad spec-comment frame", "err", err)
		return
	}
	if err := c.comments.Apply(ctx, p, ev); err != nil {
		c.log.Debug("coordinator: spec-comment op rejected", "op", ev.Op, "err", err)
	}
}

// pinger drives liveness: it pings the peer every pingInterval and cancels the
// connection if a ping is not answered within livenessTimeout, which unblocks
// the read loop and drops the instance from the registry.
func (c *Coordinator) pinger(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, livenessTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

// wsSender adapts a WebSocket connection to the registry's Sender interface so
// capability code can push a frame to an instance without touching socket
// plumbing. Writes are serialized: the WebSocket allows only one writer at a
// time, and fan-out may call Send from several goroutines.
type wsSender struct {
	conn *websocket.Conn
	ctx  context.Context
	mu   sync.Mutex
}

// Send marshals v to JSON and writes it as a text frame.
func (s *wsSender) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Write(s.ctx, websocket.MessageText, b)
}
