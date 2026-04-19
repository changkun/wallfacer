#!/usr/bin/env bash
# e2e-switch-org.sh — exercise the full org-switching loop:
#
#   1. Sign in via email OTP (shared cookie jar)
#   2. Observe the default view  (personal after v0.5.8)
#   3. POST /api/auth/switch-org with the first available org
#   4. Follow the redirect → new session
#   5. Observe view is now that org
#   6. POST /api/auth/switch-org with org_id=""
#   7. Follow the redirect → another new session
#   8. Observe view is personal again
#
# Every step asserts /api/auth/orgs current_id matches expectations.
# Exit 0 = all pass. Non-zero = something didn't behave.
#
# Usage:
#   WF_URL=http://localhost:8080 sh scripts/e2e-switch-org.sh

set -eu

WF_URL="${WF_URL:-http://localhost:8080}"
COOKIE_JAR="$(mktemp -t wf-e2e-switch-XXXXXX)"
trap 'rm -f "$COOKIE_JAR"' EXIT

command -v jq >/dev/null || { echo "jq required; brew install jq" >&2; exit 1; }

pass=0; fail=0
ok()   { printf "  \033[32m✓\033[0m %s\n" "$1"; pass=$((pass+1)); }
bad()  { printf "  \033[31m✗\033[0m %s\n" "$1" >&2; fail=$((fail+1)); }
info() { printf "  \033[2mⓘ\033[0m %s\n" "$1"; }
step() { printf "\n\033[1m=== %s ===\033[0m\n" "$1"; }

parse_location() {
  awk 'tolower($1) == "location:" { sub(/^[^:]*:[[:space:]]*/,"",$0); sub(/\r$/,"",$0); print; exit }'
}
parse_form_csrf() {
  awk 'match($0, /name="csrf_token"[^>]*value="[^"]*"/) { s=substr($0,RSTART,RLENGTH); sub(/.*value="/,"",s); sub(/".*/,"",s); print s; exit }'
}

# ─── 0: read config ─────────────────────────────────────────────────────────
step "0. Read wallfacer config"
cfg="$(curl -sS "$WF_URL/api/config")" || { bad "can't reach $WF_URL"; exit 1; }
cloud="$(printf '%s' "$cfg" | jq -r '.cloud // false')"
[ "$cloud" = "true" ] && ok "cloud mode on" || { bad "cloud=false"; exit 1; }
AUTH_URL="$(printf '%s' "$cfg" | jq -r '.auth_url // "https://auth.latere.ai"')"
info "wallfacer: $WF_URL"
info "auth:      $AUTH_URL"

# ─── 1: sign in via email OTP ───────────────────────────────────────────────
step "1. Sign in (email OTP flow)"
auth_url="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$WF_URL/login" | parse_location)"
[ -n "$auth_url" ] || { bad "no redirect from $WF_URL/login"; exit 1; }
login_url="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$auth_url" | parse_location)"
case "$login_url" in
  /login*|"$AUTH_URL"/login*) ok "authorize → login (no SSO session yet)";;
  *)
    info "authorize redirect: $login_url"
    info "(already have SSO session? will try to pick up at callback.)"
    # If we already have an SSO session, this URL points at /callback
    # with a code. Follow it to land the wallfacer session cookie.
    if echo "$login_url" | grep -q "$WF_URL/callback"; then
      cb_home="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$login_url" | parse_location)"
      [ -n "$cb_home" ] && ok "callback reused existing SSO session" || { bad "callback had no location"; exit 1; }
      # Skip the email OTP dance.
      OTP_SKIPPED=1
    fi
    ;;
esac

if [ -z "${OTP_SKIPPED:-}" ]; then
  csrf="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$AUTH_URL/login/email" | parse_form_csrf)"
  [ -n "$csrf" ] && ok "got CSRF token" || { bad "no CSRF"; exit 1; }

  printf "\n  email: "; read -r EMAIL
  send_body_file="$(mktemp -t wf-e2e-body-XXXXXX)"
  send_status="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
    --data-urlencode "email=$EMAIL" \
    --data-urlencode "csrf_token=$csrf" \
    -o "$send_body_file" -w "%{http_code}" \
    "$AUTH_URL/login/email")"
  [ "$send_status" = "200" ] || { bad "send-code HTTP $send_status"; exit 1; }
  ok "send-code accepted"
  csrf2="$(parse_form_csrf < "$send_body_file")"
  rm -f "$send_body_file"
  csrf2="${csrf2:-$csrf}"

  printf "\n  6-digit code from your inbox: "; read -r CODE
  verify_url="$(curl -sS -D - -o /dev/null -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
    --data-urlencode "email=$EMAIL" \
    --data-urlencode "code=$CODE" \
    --data-urlencode "csrf_token=$csrf2" \
    "$AUTH_URL/login/email/verify" | parse_location)"
  [ -n "$verify_url" ] || { bad "verify failed (wrong code?)"; exit 1; }
  ok "code accepted, SSO session live"

  case "$verify_url" in http*) authz2="$verify_url";; *) authz2="$AUTH_URL$verify_url";; esac
  cb_url="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$authz2" | parse_location)"
  [ -n "$cb_url" ] && ok "authorize → callback" || { bad "authorize after SSO had no redirect"; exit 1; }
  curl -sS -o /dev/null -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$cb_url"
  ok "wallfacer session cookie set"
fi

# Helper: read /api/auth/orgs with current cookies, print summary, set globals.
current_id=""
orgs_count=0
first_org_id=""
first_org_name=""
read_orgs() {
  local resp
  resp="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$WF_URL/api/auth/orgs")"
  orgs_count="$(printf '%s' "$resp" | jq -r '.orgs | length // 0')"
  current_id="$(printf '%s' "$resp" | jq -r '.current_id // ""')"
  first_org_id="$(printf '%s' "$resp" | jq -r '.orgs[0].id // ""')"
  first_org_name="$(printf '%s' "$resp" | jq -r '.orgs[0].name // ""')"
}

# read_groups: queries /api/config and sets wscount / wsnames to the
# workspace-group list visible in the current view. The real test of
# strict isolation: personal and org views must return different
# counts (and disjoint contents for groups exclusive to one view).
wscount=0
wsnames=""
read_groups() {
  local resp
  resp="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$WF_URL/api/config")"
  wscount="$(printf '%s' "$resp" | jq -r '(.workspace_groups // []) | length')"
  wsnames="$(printf '%s' "$resp" | jq -r '[(.workspace_groups // [])[] | (.name // (.workspaces[0] // ""))] | join(", ")')"
}

# perform_switch <target_org_id>: calls /api/auth/switch-org and follows
# the returned redirect_url through the OAuth dance back to a fresh
# wallfacer session cookie.
perform_switch() {
  local target="$1"
  local resp redirect
  resp="$(curl -sS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"org_id\":\"$target\"}" \
    "$WF_URL/api/auth/switch-org")"
  redirect="$(printf '%s' "$resp" | jq -r '.redirect_url // ""')"
  [ -n "$redirect" ] || { bad "no redirect_url in switch response: $resp"; return 1; }
  # Step a: /login?org_id=<target>
  local a_url
  a_url="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$WF_URL$redirect" | parse_location)"
  [ -n "$a_url" ] || { bad "$WF_URL$redirect had no location"; return 1; }
  # Step b: /authorize (with SSO session still live, carries org_id)
  case "$a_url" in http*) ;; *) a_url="$AUTH_URL$a_url";; esac
  local cb_url
  cb_url="$(curl -sS -o /dev/null -D - -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$a_url" | parse_location)"
  [ -n "$cb_url" ] || { bad "authorize had no location: $a_url"; return 1; }
  # Step c: /callback → session cookie refreshed
  curl -sS -o /dev/null -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$cb_url"
}

# ─── 2: default view ────────────────────────────────────────────────────────
step "2. Default view after login"
read_orgs
read_groups
info "orgs available:   $orgs_count"
info "current_id:       ${current_id:-(empty — personal)}"
info "workspace groups: $wscount ${wsnames:+— $wsnames}"
if [ "$orgs_count" -lt 1 ]; then
  bad "no orgs available; can't exercise switching. Add at least one org_members row for this user."
  exit 1
fi
if [ -n "$current_id" ]; then
  info "default is an org (active_org_id already set from prior switch)."
else
  ok "default is Personal (active_org_id is NULL)"
fi
default_current="$current_id"
default_wscount="$wscount"
default_wsnames="$wsnames"

# ─── 3: switch to first org ─────────────────────────────────────────────────
step "3. POST /api/auth/switch-org → first org ($first_org_name)"
if perform_switch "$first_org_id"; then ok "switched to org"; else exit 1; fi
read_orgs
read_groups
info "current_id:       ${current_id:-(empty)}"
info "workspace groups: $wscount ${wsnames:+— $wsnames}"
if [ "$current_id" = "$first_org_id" ]; then
  ok "current_id matches target"
else
  bad "expected current_id=$first_org_id, got '$current_id'"
fi
org_wscount="$wscount"
org_wsnames="$wsnames"

# ─── 4: switch back to personal ─────────────────────────────────────────────
step "4. POST /api/auth/switch-org → Personal"
if perform_switch ""; then ok "switched to personal"; else exit 1; fi
read_orgs
read_groups
info "current_id:       ${current_id:-(empty)}"
info "workspace groups: $wscount ${wsnames:+— $wsnames}"
if [ -z "$current_id" ]; then
  ok "current_id is empty (personal)"
else
  bad "expected personal (empty current_id), got '$current_id'"
fi
personal_wscount="$wscount"
personal_wsnames="$wsnames"

# ─── 5: switch back to the org again ────────────────────────────────────────
step "5. POST /api/auth/switch-org → org again"
if perform_switch "$first_org_id"; then ok "switched back to org"; else exit 1; fi
read_orgs
read_groups
info "current_id:       ${current_id:-(empty)}"
info "workspace groups: $wscount ${wsnames:+— $wsnames}"
if [ "$current_id" = "$first_org_id" ]; then
  ok "current_id matches after round-trip"
else
  bad "round-trip lost org: expected $first_org_id, got '$current_id'"
fi

# ─── 6: contrast: personal vs org workspace-group lists ─────────────────────
step "6. Verify strict isolation by workspace-group counts"
info "personal view:  $personal_wscount groups  ($personal_wsnames)"
info "org view:       $org_wscount groups  ($org_wsnames)"
if [ "$personal_wsnames" = "$org_wsnames" ] && [ "$personal_wscount" -gt 0 ]; then
  bad "personal and org views show IDENTICAL workspace groups — isolation is NOT working"
  info "  personal groups leaked into org view, or vice versa."
elif [ "$personal_wscount" = "0" ] && [ "$org_wscount" = "0" ]; then
  info "both views empty — can't confirm isolation here. Create a personal workspace group + an org-scoped one and re-run."
else
  ok "personal and org views differ (expected under strict isolation)"
fi

# ─── Summary ────────────────────────────────────────────────────────────────
step "Summary"
echo "  pass: $pass    fail: $fail"
[ "$fail" -eq 0 ]
