import { describe, expect, it } from 'vitest';
import type { Principal } from 'latere-ui';
import { accountRole } from './accountRole';

const base: Principal = {
  principal_id: 'u1',
  email: 'ada@latere.ai',
  org_id: '',
  orgs: [],
};

describe('accountRole', () => {
  it('platform_admin badges the cross-org admin, whatever the org', () => {
    expect(accountRole({ ...base, roles: ['platform_admin'] })).toBe('platform_admin');
    expect(accountRole({ ...base, org_id: 'org_1', roles: ['platform_admin', 'member'] })).toBe(
      'platform_admin',
    );
  });

  it('no org and no platform role is the individual tenant', () => {
    expect(accountRole(base)).toBe('individual');
  });

  it('an org member without platform_admin gets no fabricated tier', () => {
    expect(accountRole({ ...base, org_id: 'org_1', roles: ['member'] })).toBeUndefined();
  });

  // Regression (id-09): the retired is_superadmin flag confers nothing; only the
  // roles claim does. A token carrying the dead flag is not an admin.
  it('the retired is_superadmin flag is ignored', () => {
    expect(accountRole({ ...base, org_id: 'org_1', is_superadmin: true })).toBeUndefined();
  });
});
