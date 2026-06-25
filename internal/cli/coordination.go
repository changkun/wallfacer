package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/oidc"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/coordinator/client"
	"latere.ai/x/wallfacer/internal/gitutil"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/speccomment"
	"latere.ai/x/wallfacer/internal/store/postgres"
	"latere.ai/x/wallfacer/internal/workspace"
)

// defaultCoordinationURL is the wallfacerd coordinator endpoint a local instance
// dials. Overridable with WALLFACER_COORDINATION_URL for staging or self-hosted
// deployments.
const defaultCoordinationURL = "wss://wf.latere.ai/api/coordination/ws"

// coordinationGate holds the runtime opt-in switch. It is the data-boundary
// gate: while closed (the default), the connector dials nothing and emits
// nothing. The settings UI flips it without restarting the connector, which
// re-reads it every cycle. The choice persists to a flag file so it survives a
// restart.
type coordinationGate struct {
	optedIn atomic.Bool
	path    string // persistence file; empty disables persistence

	// connected and signedIn report live state for the status surface, set once
	// the connector exists. Nil-safe: a disabled connector reports false for both.
	connected func() bool
	signedIn  func() bool
}

func (g *coordinationGate) OptedIn() bool { return g.optedIn.Load() }

// Connected reports whether the outbound WebSocket to the coordinator is live.
func (g *coordinationGate) Connected() bool { return g.connected != nil && g.connected() }

// SignedIn reports whether a usable token exists for the coordination connection.
func (g *coordinationGate) SignedIn() bool { return g.signedIn != nil && g.signedIn() }

// SetOptedIn flips the gate and persists the choice. Persistence failure is
// logged, not fatal: the in-memory state still governs the connector.
func (g *coordinationGate) SetOptedIn(v bool) {
	g.optedIn.Store(v)
	if g.path == "" {
		return
	}
	data := []byte("0")
	if v {
		data = []byte("1")
	}
	if err := os.WriteFile(g.path, data, 0o600); err != nil {
		logger.Main.Warn("coordination: persist opt-in failed", "err", err)
	}
}

// loadOptIn seeds the gate: a persisted in-app choice wins if present, else the
// server-side default. The default is ON for a signed-in instance (a deliberate
// product decision: collaboration on by default once signed in), overridable to
// off with WALLFACER_COORDINATION=0 or the in-app toggle. The connection still
// requires sign-in, so an anonymous instance phones home nothing regardless
// (the data-boundary floor that remains).
func loadOptIn(path string) bool {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)) == "1"
	}
	return coordinationDefault()
}

// startCoordinationClient wires and runs the outbound coordination connector in
// a goroutine. It returns the gate so the settings layer can toggle opt-in. The
// connector self-gates on sign-in (a stored token) and opt-in, so calling this
// unconditionally is safe: nothing dials until both hold.
func startCoordinationClient(ctx context.Context, configDir string, wsMgr *workspace.Manager, relay *handler.CommentRelay, tokenStore authkit.TokenStore, authCfg authConfigForRefresh, logger *slog.Logger) *coordinationGate {
	gate := &coordinationGate{path: filepath.Join(configDir, "coordination-opt-in")}
	gate.optedIn.Store(loadOptIn(gate.path))

	if tokenStore == nil {
		logger.Warn("coordination: token store unavailable; connector disabled")
		return gate
	}
	instanceID, err := coordinator.LoadOrCreateInstanceID(configDir)
	if err != nil {
		logger.Warn("coordination: instance id unavailable; connector disabled", "err", err)
		return gate
	}

	// oidcClient refreshes an expired access token using the stored refresh
	// token, so a long-running instance stays connected past token expiry.
	oidcClient := oidc.New(oidc.Config{AuthURL: authCfg.AuthURL, ClientID: authCfg.ClientID})

	url := os.Getenv("WALLFACER_COORDINATION_URL")
	if url == "" {
		url = defaultCoordinationURL
	}

	hostLabel, _ := os.Hostname()
	if hostLabel == "" {
		hostLabel = "wallfacer"
	}
	version := Version
	if version == "" {
		version = "dev"
	}

	tokenFunc := coordinationTokenFunc(ctx, tokenStore, oidcClient)
	gate.signedIn = func() bool { _, ok := tokenFunc(); return ok }
	cfg := client.Config{
		URL:      url,
		Token:    tokenFunc,
		OptedIn:  gate.OptedIn,
		Manifest: coordinationManifestFunc(instanceID, hostLabel, version, wsMgr),
		Logger:   logger,
	}
	// Wire the comment relay to the connection: coordinator pushes flow into the
	// relay (cache + browser SSE), browser ops flow up via the connector's Send.
	if relay != nil {
		cfg.OnInbound = relay.HandleInbound
	}
	connector := client.NewConnector(cfg)
	gate.connected = connector.Connected
	if relay != nil {
		// Translate "no live connection" into the handler's transient-unavailable
		// error so a browser op while disconnected surfaces as 503 (retry), not a
		// 502 bad-gateway.
		relay.SetSendUp(func(ev speccomment.Event) error {
			err := connector.Send(ev)
			if errors.Is(err, client.ErrNotConnected) {
				return handler.ErrCoordinatorUnavailable
			}
			return err
		})
	}
	go connector.Run(ctx)
	return gate
}

// newCoordinationTokenStore opens the persisted token store the connector reads
// and the session bridge writes (the same path `wallfacer auth login` and the
// latere CLI use). Returns nil on failure; the connector then stays disabled.
func newCoordinationTokenStore() authkit.TokenStore {
	p, err := authkit.DefaultFileTokenStorePath()
	if err != nil {
		return nil
	}
	store, err := authkit.NewFileTokenStore(p)
	if err != nil {
		return nil
	}
	return store
}

// sessionTokenBridge persists a browser cookie session's token into the
// connector's token store. The UI "Sign in" uses the OIDC cookie flow, which
// authenticates the browser but never writes the device-code token.json the
// connector reads, so without this bridge a user who signed in via the UI would
// still see "coordination unavailable". On any authenticated request the bridge
// copies the session's (already-refreshed) access + refresh token to the store,
// so signing in via the UI enables the outbound connection automatically.
type sessionTokenBridge struct {
	client *oidc.Client
	store  authkit.TokenStore
	last   atomic.Value // string: last access token written, to skip redundant saves
}

func newSessionTokenBridge(client *oidc.Client, store authkit.TokenStore) *sessionTokenBridge {
	return &sessionTokenBridge{client: client, store: store}
}

// wrap returns middleware that mirrors the cookie session token into the store.
// A nil client or store (auth not configured) returns next unchanged, and an
// anonymous request (no decryptable session) is a no-op, so local-anonymous
// behavior is byte-identical.
func (b *sessionTokenBridge) wrap(next http.Handler) http.Handler {
	if b == nil || b.client == nil || b.store == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.capture(r)
		next.ServeHTTP(w, r)
	})
}

func (b *sessionTokenBridge) capture(r *http.Request) {
	sess, err := b.client.GetSession(r)
	if err != nil || sess == nil {
		return
	}
	b.sync(sess.AccessToken, sess.RefreshToken, sess.Expiry)
}

// sync writes the session's token to the store, skipping the file I/O when the
// access token is unchanged since the last write. Split from capture so the
// dedup + save path is unit-testable without a real cookie session.
func (b *sessionTokenBridge) sync(accessToken, refreshToken string, expiry time.Time) {
	if accessToken == "" {
		return
	}
	if last, ok := b.last.Load().(string); ok && last == accessToken {
		return
	}
	_ = b.store.Save(&oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
	})
	b.last.Store(accessToken)
}

// authConfigForRefresh is the subset of auth config the connector needs to
// refresh tokens. Declared locally so coordination.go does not import the auth
// package's full Config shape.
type authConfigForRefresh struct {
	AuthURL  string
	ClientID string
}

// coordinationTokenFunc returns a Token callback that loads the persisted
// device-code token (the same token.json the local board's sign-in writes) and
// refreshes it when expired. Returns ("", false) when signed out so the
// connector stays idle.
func coordinationTokenFunc(ctx context.Context, store authkit.TokenStore, oidcClient *oidc.Client) func() (string, bool) {
	return func() (string, bool) {
		tok, err := store.Load()
		if err != nil || tok == nil || tok.AccessToken == "" {
			return "", false
		}
		if tok.Valid() {
			return tok.AccessToken, true
		}
		if tok.RefreshToken != "" && oidcClient != nil {
			if nt, err := oidcClient.RefreshTokenContext(ctx, tok.RefreshToken); err == nil && nt != nil && nt.AccessToken != "" {
				_ = store.Save(nt)
				return nt.AccessToken, true
			}
		}
		return "", false
	}
}

// coordinationManifestFunc builds the registration manifest from the instance's
// currently active workspaces. Each workspace path's git origin is resolved
// client-side and normalized to the canonical host/owner/repo cross-machine
// join key; local folder paths never leave the machine. A folder with no remote
// contributes no repo identity and never joins org collaboration.
func coordinationManifestFunc(instanceID, hostLabel, version string, wsMgr *workspace.Manager) func() coordinator.Manifest {
	caps := []string{"comments"}
	return func() coordinator.Manifest {
		var refs []coordinator.WorkspaceRef
		seen := make(map[string]bool)
		if wsMgr != nil {
			for _, snap := range wsMgr.AllActiveSnapshots() {
				// LocalKey must be an opaque hash, never a path: GroupKey joins the
				// raw local folder paths, so hash it before it crosses the wire
				// (the data boundary forbids local paths). The coordinator ignores
				// it anyway; it joins on the canonical remote.
				localKey := hashLocalKey(workspace.GroupKey(snap.Workspaces))
				for _, path := range snap.Workspaces {
					remote := coordinator.NormalizeRemoteURL(gitutil.WorkspaceStatus(path).RemoteURL)
					if remote == "" || seen[remote] {
						continue
					}
					seen[remote] = true
					refs = append(refs, coordinator.WorkspaceRef{Remote: remote, LocalKey: localKey})
				}
			}
		}
		return coordinator.NewManifest(instanceID, hostLabel, version, refs, caps)
	}
}

// newCommentStore selects the coordinator's authoritative comment store. With
// WALLFACER_DATABASE_URL set it uses the durable Postgres store (the system of
// record for the cloud-authoritative comments); otherwise an in-memory store
// (single-replica dev, byte-identical for one process). A Postgres failure
// falls back to memory rather than taking the web server down, since comments
// are best-effort infrastructure.
//
// Any fallback to memory is logged at Error: an in-memory store loses comments
// on restart and shares nothing across replicas, so a misconfigured cloud
// coordinator must be loud, not silent. The unset-DSN path once reached prod
// unnoticed precisely because it logged nothing.
func newCommentStore(ctx context.Context) coordinator.CommentStore {
	dsn := os.Getenv("WALLFACER_DATABASE_URL")
	var openErr error
	if dsn != "" {
		// The shared store owns the pool and runs migrations; the comment store
		// borrows the pool. The pool lives for the process, the same lifetime the
		// inline-schema store had.
		st, err := postgres.New(ctx, dsn)
		if err == nil {
			logger.Main.Info("coordination: using Postgres comment store")
			return coordinator.NewPostgresCommentStore(st.Pool())
		}
		openErr = err
	}
	logger.Main.Error("coordination: spec comments NON-DURABLE (in-memory store)",
		"reason", commentStoreFallbackReason(dsn, openErr))
	return coordinator.NewMemCommentStore()
}

// commentStoreFallbackReason explains why the coordinator runs without a durable
// comment store, or "" when it has one. A non-empty result means spec comments
// are in-memory only: lost on restart and not shared across replicas.
func commentStoreFallbackReason(dsn string, openErr error) string {
	switch {
	case dsn == "":
		return "WALLFACER_DATABASE_URL unset: comments are in-memory only (lost on restart, not shared across replicas)"
	case openErr != nil:
		return "Postgres comment store unavailable, using in-memory: " + openErr.Error()
	default:
		return ""
	}
}

// hashLocalKey turns the path-bearing workspace.GroupKey into an opaque hex
// digest so no local filesystem path crosses the wire (the data boundary). The
// hash is stable per machine, which is all the instance's own routing needs.
func hashLocalKey(groupKey string) string {
	sum := sha256.Sum256([]byte(groupKey))
	return hex.EncodeToString(sum[:])
}

// coordinationDefault reads the server-side default for a signed-in instance.
// It defaults ON (deliberate product decision); WALLFACER_COORDINATION=0 (or
// false/off/no) forces it off. The in-app toggle persists a per-instance
// override that wins (see loadOptIn). Anonymous instances never connect
// regardless, so this only governs signed-in instances with no explicit choice.
func coordinationDefault() bool {
	switch os.Getenv("WALLFACER_COORDINATION") {
	case "0", "false", "FALSE", "no", "off":
		return false
	default:
		return true
	}
}
