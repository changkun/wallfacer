#!/usr/bin/env bash
# e2e-auth-flow.sh — drive the full cloud-mode auth chain interactively.
#
# Walks through: wallfacer /login → auth /authorize → email OTP →
# /callback → wallfacer session → /api/auth/me → /api/auth/orgs.
# Asserts every hop along the way.
#
# Prereq: a running local wallfacer with WALLFACER_CLOUD=true and
# AUTH_URL pointing at a reachable auth service. Default:
#   http://localhost:8080  (wallfacer)
#   https://auth.latere.ai (auth; read from wallfacer's /api/config)
#
# Usage:
#   sh scripts/e2e-auth-flow.sh
#   WF_URL=http://localhost:9090 sh scripts/e2e-auth-flow.sh
#
# The script will:
#   1. Prompt you for an email address.
#   2. Trigger auth.latere.ai's email-OTP send.
#   3. Prompt you for the 6-digit code that lands in your inbox.
#   4. Complete the login and verify every downstream endpoint.
#
# Exit codes: 0 all checks pass, 1 something failed.

set -eu

WF_URL="${WF_URL:-http://localhost:8080}"
COOKIE_JAR="${COOKIE_JAR:-$(mktemp -t wf-e2e-XXXXXX)}"
trap 'rm -f "$COOKIE_JAR"' EXIT

command -v jq >/dev/null || { echo "jq required; brew install jq" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl required" >&2; exit 1; }

pass=0; fail=0
ok()   { printf "  \033[32m✓\033[0m %s\n" "$1"; pass=$((pass+1)); }
bad()  { printf "  \033[31m✗\033[0m %s\n" "$1" >&2; fail=$((fail+1)); }
info() { printf "  \033[2mⓘ\033[0m %s\n" "$1"; }
step() { printf "\n\033[1m=== %s ===\033[0m\n" "$1"; }

# curl helpers that share a cookie jar across the whole flow.
jc() {  # jar curl — follows no redirects, returns status only
  curl -sS -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$@"
}
jcb() { # body + status; reads body into $1 (variable name), sets http_code into $2
  local _body _status
  _body="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -w $'\n%{http_code}' "$@")"
  _status="${_body##*$'\n'}"
  _body="${_body%$'\n'*}"
  eval "$1=\$_body"
  eval "$2=\$_status"
}
jloc() { # follow location manually — returns Location header value or empty
  curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$@" |
    awk 'tolower($1) == "location:" { sub(/^[^:]*:[[:space:]]*/,"",$0); sub(/\r$/,"",$0); print; exit }'
}

step "0. Read wallfacer config"
cfg="$(curl -sS "$WF_URL/api/config")" || { bad "can't reach $WF_URL"; exit 1; }
cloud="$(printf '%s' "$cfg" | jq -r '.cloud // false')"
[ "$cloud" = "true" ] && ok "cloud mode is on" || { bad "cloud=false — set WALLFACER_CLOUD=true"; exit 1; }
AUTH_URL="$(printf '%s' "$cfg" | jq -r '.auth_url // empty')"
[ -n "$AUTH_URL" ] || AUTH_URL="https://auth.latere.ai"
info "wallfacer: $WF_URL"
info "auth:      $AUTH_URL"

step "1. wallfacer /login → auth /authorize (chase redirect)"
# Fetch /login; wallfacer's HandleLogin sets __Host-latere-flow and 302s to authorize.
auth_url="$(jloc "$WF_URL/login")"
case "$auth_url" in
  "$AUTH_URL/authorize"*) ok "302 to $AUTH_URL/authorize";;
  *) bad "unexpected redirect: $auth_url"; exit 1;;
esac

step "2. /authorize (no auth session yet) → /login"
# Capture both status and Location so we can see what auth actually said.
authz_headers="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$auth_url")"
authz_status="$(printf '%s' "$authz_headers" | awk 'NR==1{print $2; exit}')"
login_url="$(printf '%s' "$authz_headers" | awk 'tolower($1) == "location:" { sub(/^[^:]*:[[:space:]]*/,"",$0); sub(/\r$/,"",$0); print; exit }')"
info "authorize status: $authz_status"
info "authorize Location: ${login_url:-(none)}"
case "$login_url" in
  /login|/login\?*) ok "redirected to auth /login";;
  "")
    bad "no redirect from /authorize — response was $authz_status"
    echo "  Dump of full response (first 800 chars of body):"
    curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$auth_url" | head -c 800
    echo
    echo "  Likely causes:"
    echo "    • wallfacer's client_id/redirect_uri not registered on auth (/admin check)"
    echo "    • fosite returned 400 with an error page instead of the usual 302 → /login"
    echo "    • The authorize URL contains an unexpected param"
    exit 1;;
  *) bad "unexpected redirect: $login_url"; exit 1;;
esac

step "3. GET auth /login/email → CSRF cookie + form"
csrf_form="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$AUTH_URL/login/email" |
  awk 'match($0, /name="csrf_token"[^>]*value="[^"]*"/) { s=substr($0,RSTART,RLENGTH); sub(/.*value="/,"",s); sub(/".*/,"",s); print s; exit }')"
[ -n "$csrf_form" ] && ok "got CSRF token from form" || { bad "no csrf_token in email form"; exit 1; }

printf "\n  email: "; read -r EMAIL
[ -n "$EMAIL" ] || { bad "email required"; exit 1; }

step "4. POST /login/email — trigger OTP send"
send_status="$(jc -X POST \
  --data-urlencode "email=$EMAIL" \
  --data-urlencode "csrf_token=$csrf_form" \
  "$AUTH_URL/login/email")"
case "$send_status" in
  200|302) ok "send-code accepted (HTTP $send_status)";;
  *) bad "send-code failed (HTTP $send_status)"; exit 1;;
esac

# The response HTML renders a fresh CSRF token for the verify form.
csrf_verify="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
  --data-urlencode "email=$EMAIL" \
  --data-urlencode "csrf_token=$csrf_form" \
  "$AUTH_URL/login/email" |
  awk 'match($0, /name="csrf_token"[^>]*value="[^"]*"/) { s=substr($0,RSTART,RLENGTH); sub(/.*value="/,"",s); sub(/".*/,"",s); print s; exit }')"
[ -n "$csrf_verify" ] && ok "verify CSRF token ready" || info "no second CSRF (re-using first)"

printf "\n  6-digit code from your inbox: "; read -r CODE
[ -n "$CODE" ] || { bad "code required"; exit 1; }

step "5. POST /login/email/verify — complete SSO login"
verify_url="$(curl -sS -D - -o /dev/null -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
  --data-urlencode "email=$EMAIL" \
  --data-urlencode "code=$CODE" \
  --data-urlencode "csrf_token=${csrf_verify:-$csrf_form}" \
  "$AUTH_URL/login/email/verify" |
  awk 'tolower($1) == "location:" { sub(/^[^:]*:[[:space:]]*/,"",$0); sub(/\r$/,"",$0); print; exit }')"
case "$verify_url" in
  /authorize*|"$AUTH_URL"/authorize*) ok "code verified, SSO session established";;
  "") bad "no redirect from /login/email/verify — check the code";;
  *) bad "unexpected redirect: $verify_url";;
esac

step "6. Re-enter /authorize with SSO session → wallfacer /callback"
# verify_url might be a relative /authorize; resolve against AUTH_URL.
case "$verify_url" in
  http*) authz2="$verify_url";;
  *)     authz2="$AUTH_URL$verify_url";;
esac
cb_url="$(jloc "$authz2")"
case "$cb_url" in
  "$WF_URL/callback"*) ok "auth issued code, redirecting to wallfacer /callback";;
  *) bad "expected callback, got: $cb_url"; exit 1;;
esac

step "7. /callback exchange → wallfacer session cookie"
home_url="$(jloc "$cb_url")"
case "$home_url" in
  /|"$WF_URL"/|"$WF_URL") ok "callback landed, session cookie set";;
  *) bad "unexpected callback redirect: $home_url";;
esac

step "8. /api/auth/me (signed-in shape)"
jcb body status "$WF_URL/api/auth/me"
case "$status" in
  200)
    ok "200 OK"
    info "sub:   $(printf '%s' "$body" | jq -r '.sub')"
    info "email: $(printf '%s' "$body" | jq -r '.email')"
    info "name:  $(printf '%s' "$body" | jq -r '.name // "(empty)"')"
    ;;
  *) bad "want 200, got $status";;
esac

step "9. /api/auth/orgs (the real test)"
jcb body status "$WF_URL/api/auth/orgs"
case "$status" in
  200)
    count="$(printf '%s' "$body" | jq '.orgs | length')"
    ok "200 OK — $count org(s)"
    info "names: $(printf '%s' "$body" | jq -r '[.orgs[].name] | join(", ")')"
    info "current_id: $(printf '%s' "$body" | jq -r '.current_id // "(none)"')"
    ;;
  204) info "204 — no memberships for this account (expected for a brand-new user)";;
  *) bad "want 200|204, got $status — body: $body";;
esac

step "Summary"
echo "  pass: $pass    fail: $fail"
[ "$fail" -eq 0 ]
