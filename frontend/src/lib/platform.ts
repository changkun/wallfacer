// Platform detection for the install page. Maps a browser environment to the
// matching release asset so visitors land on the right download without
// reading a table. OS is derived synchronously from the user-agent string;
// arch is refined asynchronously from navigator.userAgentData when present,
// since the UA string alone cannot distinguish Apple Silicon from Intel.

const REPO = 'changkun/wallfacer';

export type OS = 'darwin' | 'windows' | 'linux' | 'unknown';
export type Arch = 'arm64' | 'amd64';

export interface Platform {
  os: OS;
  arch: Arch;
}

export interface PlatformMeta {
  id: OS;
  label: string;
  // archs lists the architectures we publish a binary for, recommended first.
  archs: Arch[];
}

// PLATFORMS mirrors the published release assets. Windows ships amd64 only.
export const PLATFORMS: PlatformMeta[] = [
  { id: 'darwin', label: 'macOS', archs: ['arm64', 'amd64'] },
  { id: 'windows', label: 'Windows', archs: ['amd64'] },
  { id: 'linux', label: 'Linux', archs: ['amd64', 'arm64'] },
];

// osLabel returns the brand-cased OS name.
export function osLabel(os: OS): string {
  return PLATFORMS.find((p) => p.id === os)?.label ?? 'your platform';
}

// archLabel returns a human label for an architecture. macOS uses the
// marketing names (Apple Silicon / Intel) since that is how users think of it.
export function archLabel(os: OS, arch: Arch): string {
  if (os === 'darwin') return arch === 'arm64' ? 'Apple Silicon' : 'Intel';
  return arch === 'arm64' ? 'ARM64' : 'x86_64';
}

// assetName returns the release asset filename for an os/arch pair.
export function assetName(os: OS, arch: Arch): string {
  const ext = os === 'windows' ? '.exe' : '';
  return `wallfacer-${os}-${arch}${ext}`;
}

// downloadURL returns the GitHub "latest release" direct-download URL for an
// os/arch pair, so the link never goes stale across releases.
export function downloadURL(os: OS, arch: Arch): string {
  return `https://github.com/${REPO}/releases/latest/download/${assetName(os, arch)}`;
}

// defaultArch returns the most likely architecture when high-entropy data is
// unavailable: Apple Silicon dominates new Macs, amd64 everywhere else.
export function defaultArch(os: OS): Arch {
  return os === 'darwin' ? 'arm64' : 'amd64';
}

// clampArch coerces an architecture to one we actually publish for the OS,
// falling back to the recommended (first) arch. Windows arm collapses to amd64.
export function clampArch(os: OS, arch: Arch): Arch {
  const meta = PLATFORMS.find((p) => p.id === os);
  if (!meta) return arch;
  return meta.archs.includes(arch) ? arch : meta.archs[0];
}

// detectOS parses a user-agent string. Wallfacer is desktop-only, so mobile
// agents resolve to 'unknown' and the page shows every download instead.
export function detectOS(ua: string): OS {
  const s = ua || '';
  if (/android/i.test(s)) return 'unknown';
  if (/iphone|ipad|ipod/i.test(s)) return 'unknown';
  if (/mac os x|macintosh/i.test(s)) return 'darwin';
  if (/windows/i.test(s)) return 'windows';
  if (/linux|x11/i.test(s)) return 'linux';
  return 'unknown';
}

// archFromUAData maps the architecture field returned by
// navigator.userAgentData.getHighEntropyValues(['architecture']) to our arch
// names, falling back to the OS default for unrecognized values.
export function archFromUAData(architecture: string | undefined, os: OS): Arch {
  if (architecture === 'arm') return 'arm64';
  if (architecture === 'x86') return 'amd64';
  return defaultArch(os);
}

// detectPlatform resolves the synchronous best guess from a UA string. Arch is
// the OS default until refined by archFromUAData.
export function detectPlatform(ua: string): Platform {
  const os = detectOS(ua);
  return { os, arch: defaultArch(os) };
}
