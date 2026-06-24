package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/oidc"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/coordinator/client"
	"latere.ai/x/wallfacer/internal/gitutil"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/speccomment"
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
}

func (g *coordinationGate) OptedIn() bool { return g.optedIn.Load() }

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

// loadOptIn seeds the gate: the persisted flag file wins if present, else the
// server-side env default (off).
func loadOptIn(path string) bool {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)) == "1"
	}
	return envCoordinationOptIn()
}

// startCoordinationClient wires and runs the outbound coordination connector in
// a goroutine. It returns the gate so the settings layer can toggle opt-in. The
// connector self-gates on sign-in (a stored token) and opt-in, so calling this
// unconditionally is safe: nothing dials until both hold.
func startCoordinationClient(ctx context.Context, configDir string, wsMgr *workspace.Manager, relay *handler.CommentRelay, authCfg authConfigForRefresh, logger *slog.Logger) *coordinationGate {
	gate := &coordinationGate{path: filepath.Join(configDir, "coordination-opt-in")}
	gate.optedIn.Store(loadOptIn(gate.path))

	instanceID, err := coordinator.LoadOrCreateInstanceID(configDir)
	if err != nil {
		logger.Warn("coordination: instance id unavailable; connector disabled", "err", err)
		return gate
	}

	storePath, err := authkit.DefaultFileTokenStorePath()
	if err != nil {
		logger.Warn("coordination: token store unavailable; connector disabled", "err", err)
		return gate
	}
	tokenStore, err := authkit.NewFileTokenStore(storePath)
	if err != nil {
		logger.Warn("coordination: token store unavailable; connector disabled", "err", err)
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

	cfg := client.Config{
		URL:      url,
		Token:    coordinationTokenFunc(ctx, tokenStore, oidcClient),
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
	if relay != nil {
		relay.SetSendUp(func(ev speccomment.Event) error { return connector.Send(ev) })
	}
	go connector.Run(ctx)
	return gate
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
func newCommentStore(ctx context.Context) coordinator.CommentStore {
	if dsn := os.Getenv("WALLFACER_DATABASE_URL"); dsn != "" {
		st, err := coordinator.NewPostgresCommentStore(ctx, dsn)
		if err == nil {
			logger.Main.Info("coordination: using Postgres comment store")
			return st
		}
		logger.Main.Error("coordination: Postgres comment store unavailable; using memory", "err", err)
	}
	return coordinator.NewMemCommentStore()
}

// hashLocalKey turns the path-bearing workspace.GroupKey into an opaque hex
// digest so no local filesystem path crosses the wire (the data boundary). The
// hash is stable per machine, which is all the instance's own routing needs.
func hashLocalKey(groupKey string) string {
	sum := sha256.Sum256([]byte(groupKey))
	return hex.EncodeToString(sum[:])
}

// envCoordinationOptIn reads the server-side default for the coordination
// opt-in. The default is off (the data boundary): coordination only engages
// when explicitly enabled via WALLFACER_COORDINATION or the settings toggle.
func envCoordinationOptIn() bool {
	switch os.Getenv("WALLFACER_COORDINATION") {
	case "1", "true", "TRUE", "yes", "on":
		return true
	default:
		return false
	}
}
