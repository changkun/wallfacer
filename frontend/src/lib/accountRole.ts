import type { Principal, PlatformRole } from 'latere-ui';

// wallfacer's /api/me marshals pkg's oidc.User, whose embedded authkit.Identity
// carries `roles` since id-09 retired the is_superadmin flag. Declare it on the
// shared Principal so the account role reads the role list, not the dead field.
declare module 'latere-ui' {
  interface Principal {
    roles?: string[];
  }
}

// accountRole is the badge the shared AccountMenu renders. platform_admin is the
// cross-org role and travels on every token of that principal, whatever the org;
// with no org the caller is the individual tenant; inside an org wallfacer has no
// org-admin signal, so the tier is left unset (the dropdown shows the org name
// rather than a fabricated tier).
export function accountRole(m: Principal): PlatformRole | undefined {
  if (m.roles?.includes('platform_admin')) return 'platform_admin';
  return m.org_id ? undefined : 'individual';
}
