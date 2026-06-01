package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/oidc"
)

// RunAuth dispatches the `wallfacer auth` subcommand:
//
//	wallfacer auth login    — RFC 8628 device-code sign-in
//	wallfacer auth logout   — remove the locally stored token
//	wallfacer auth whoami   — print the saved principal_id and org_id
//
// The token is stored at <UserConfigDir>/latere/token.json, the same
// location latere-cli uses, so a single login carries over between both
// tools and (eventually) wallfacer's local-mode desktop UI.
func RunAuth(_ string, args []string) {
	if len(args) == 0 {
		printAuthUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "login":
		if err := runAuthLogin(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "wallfacer auth login:", err)
			os.Exit(1)
		}
	case "logout":
		if err := runAuthLogout(); err != nil {
			fmt.Fprintln(os.Stderr, "wallfacer auth logout:", err)
			os.Exit(1)
		}
	case "whoami":
		if err := runAuthWhoami(); err != nil {
			fmt.Fprintln(os.Stderr, "wallfacer auth whoami:", err)
			os.Exit(1)
		}
	case "-help", "--help", "-h":
		printAuthUsage()
	default:
		fmt.Fprintf(os.Stderr, "wallfacer auth: unknown command %q\n\n", args[0])
		printAuthUsage()
		os.Exit(2)
	}
}

func printAuthUsage() {
	fmt.Fprintf(os.Stderr, `Authenticate the wallfacer CLI (and local-mode desktop UI) against
auth.latere.ai using the RFC 8628 device-authorization flow.

Usage:
  wallfacer auth login    Sign in (opens a browser for confirmation)
  wallfacer auth logout   Remove the locally stored token
  wallfacer auth whoami   Print the saved principal_id and org_id

Flags (login):
  --no-browser   Do not open the verification URL automatically.
  --org=<uuid>   Sign in scoped to the given org_id ("" for personal).

The token is stored at <UserConfigDir>/latere/token.json and shared
with the latere CLI; signing in here carries over to %s.
`, "latere auth")
}

func runAuthLogin(args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ExitOnError)
	authURL := fs.String("auth-url", getenvOr("AUTH_URL", "https://auth.latere.ai"), "auth service base URL")
	clientID := fs.String("client-id", getenvOr("AUTH_CLIENT_ID", "wallfacer-cli"), "OAuth client id")
	scopes := fs.String("scopes", "openid email profile offline_access", "space-separated scopes")
	orgID := fs.String("org", "", "scope login to this org_id (empty string = personal context)")
	personal := fs.Bool("personal", false, "force personal context (equivalent to --org=\"\" being set)")
	noBrowser := fs.Bool("no-browser", false, "do not open the browser automatically")
	_ = fs.Parse(args)

	storePath, err := authkit.DefaultFileTokenStorePath()
	if err != nil {
		return fmt.Errorf("locate token store: %w", err)
	}
	store, err := authkit.NewFileTokenStore(storePath)
	if err != nil {
		return err
	}

	client := oidc.New(oidc.Config{
		AuthURL:  *authURL,
		ClientID: *clientID,
		Scopes:   splitFields(*scopes),
	})
	if client == nil {
		return errors.New("oidc: missing AuthURL or ClientID")
	}

	dcc := authkit.NewDeviceCodeClient(client, store)
	dcc.Output = os.Stderr
	if *noBrowser {
		dcc.OpenBrowser = func(string) error { return nil }
	}
	if *personal || fsSet(fs, "org") {
		dcc.ExtraParams = url.Values{"org_id": []string{*orgID}}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := dcc.Login(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Signed in. Token saved to", storePath)
	return nil
}

func runAuthLogout() error {
	storePath, err := authkit.DefaultFileTokenStorePath()
	if err != nil {
		return err
	}
	store, err := authkit.NewFileTokenStore(storePath)
	if err != nil {
		return err
	}
	if err := store.Clear(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Signed out.")
	return nil
}

func runAuthWhoami() error {
	storePath, err := authkit.DefaultFileTokenStorePath()
	if err != nil {
		return err
	}
	store, err := authkit.NewFileTokenStore(storePath)
	if err != nil {
		return err
	}
	tok, err := store.Load()
	if err != nil {
		return err
	}
	if tok == nil {
		fmt.Fprintln(os.Stderr, "not signed in (no token at", storePath+")")
		return nil
	}
	// Print a one-line summary. The access token itself is intentionally
	// not echoed so command substitution does not leak it into logs;
	// `latere auth print-token` is the supported way to retrieve it.
	fmt.Fprintf(os.Stderr, "Signed in. Token expires at %s.\n", tok.Expiry.Format("2006-01-02 15:04:05 MST"))
	return nil
}

func getenvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitFields(s string) []string {
	out := []string{}
	cur := ""
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// fsSet reports whether a flag was explicitly set on the command line.
func fsSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
