package cli

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/oidc"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/coordinator/client"
	"latere.ai/x/wallfacer/internal/gitutil"
	"latere.ai/x/wallfacer/internal/workspace"
)

// defaultCoordinationURL is the wallfacerd coordinator endpoint a local instance
// dials. Overridable with WALLFACER_COORDINATION_URL for staging or self-hosted
// deployments.
const defaultCoordinationURL = "wss://wf.latere.ai/api/coordination/ws"

// coordinationGate holds the runtime opt-in switch. It is the data-boundary
// gate: while closed (the default), the connector dials nothing and emits
// nothing. The settings UI flips it without restarting the connector, which
// re-reads it every cycle.
type coordinationGate struct {
	optedIn atomic.Bool
}

func (g *coordinationGate) OptedIn() bool  { return g.optedIn.Load() }
func (g *coordinationGate) SetOptedIn(v bool) { g.optedIn.Store(v) }

// startCoordinationClient wires and runs the outbound coordination connector in
// a goroutine. It returns the gate so the settings layer can toggle opt-in. The
// connector self-gates on sign-in (a stored token) and opt-in, so calling this
// unconditionally is safe: nothing dials until both hold.
func startCoordinationClient(ctx context.Context, configDir string, wsMgr *workspace.Manager, authCfg authConfigForRefresh, logger *slog.Logger) *coordinationGate {
	gate := &coordinationGate{}
	gate.optedIn.Store(envCoordinationOptIn())

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

	connector := client.NewConnector(client.Config{
		URL:      url,
		Token:    coordinationTokenFunc(ctx, tokenStore, oidcClient),
		OptedIn:  gate.OptedIn,
		Manifest: coordinationManifestFunc(instanceID, hostLabel, version, wsMgr),
		Logger:   logger,
	})
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
				localKey := workspace.GroupKey(snap.Workspaces)
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
