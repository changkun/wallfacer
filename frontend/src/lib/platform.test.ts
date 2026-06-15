import { describe, expect, it } from 'vitest';
import {
  archFromUAData,
  archLabel,
  assetName,
  clampArch,
  defaultArch,
  detectOS,
  detectPlatform,
  downloadURL,
  osLabel,
} from './platform';

// Real-world user-agent strings, one per browser/OS combination we care about.
const UA = {
  safariMac:
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15',
  chromeWin:
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
  firefoxLinux:
    'Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0',
  iphone:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1',
  android:
    'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36',
};

describe('detectOS', () => {
  it('detects macOS from a Safari UA', () => {
    expect(detectOS(UA.safariMac)).toBe('darwin');
  });
  it('detects Windows from a Chrome UA', () => {
    expect(detectOS(UA.chromeWin)).toBe('windows');
  });
  it('detects Linux from a Firefox UA', () => {
    expect(detectOS(UA.firefoxLinux)).toBe('linux');
  });
  it('treats iPhone as unknown (desktop-only)', () => {
    expect(detectOS(UA.iphone)).toBe('unknown');
  });
  it('treats Android as unknown even though it reports Linux', () => {
    // Regression: Android UAs contain "Linux" and must not match the linux branch.
    expect(detectOS(UA.android)).toBe('unknown');
  });
  it('returns unknown for an empty UA', () => {
    expect(detectOS('')).toBe('unknown');
  });
});

describe('defaultArch', () => {
  it('defaults macOS to Apple Silicon', () => {
    expect(defaultArch('darwin')).toBe('arm64');
  });
  it('defaults other platforms to amd64', () => {
    expect(defaultArch('windows')).toBe('amd64');
    expect(defaultArch('linux')).toBe('amd64');
  });
});

describe('archFromUAData', () => {
  it('maps arm to arm64', () => {
    expect(archFromUAData('arm', 'linux')).toBe('arm64');
  });
  it('maps x86 to amd64', () => {
    expect(archFromUAData('x86', 'darwin')).toBe('amd64');
  });
  it('falls back to the OS default for unknown values', () => {
    expect(archFromUAData(undefined, 'darwin')).toBe('arm64');
    expect(archFromUAData('mips', 'linux')).toBe('amd64');
  });
});

describe('clampArch', () => {
  it('keeps an available arch', () => {
    expect(clampArch('darwin', 'amd64')).toBe('amd64');
  });
  it('collapses Windows arm64 to the published amd64 build', () => {
    expect(clampArch('windows', 'arm64')).toBe('amd64');
  });
});

describe('assetName', () => {
  it('builds the macOS binary name', () => {
    expect(assetName('darwin', 'arm64')).toBe('wallfacer-darwin-arm64');
  });
  it('appends .exe for Windows', () => {
    expect(assetName('windows', 'amd64')).toBe('wallfacer-windows-amd64.exe');
  });
});

describe('downloadURL', () => {
  it('points at the latest-release direct download', () => {
    expect(downloadURL('linux', 'amd64')).toBe(
      'https://github.com/changkun/wallfacer/releases/latest/download/wallfacer-linux-amd64',
    );
  });
});

describe('labels', () => {
  it('uses marketing names for macOS arch', () => {
    expect(archLabel('darwin', 'arm64')).toBe('Apple Silicon');
    expect(archLabel('darwin', 'amd64')).toBe('Intel');
  });
  it('uses generic names elsewhere', () => {
    expect(archLabel('linux', 'amd64')).toBe('x86_64');
  });
  it('names the OS', () => {
    expect(osLabel('darwin')).toBe('macOS');
    expect(osLabel('unknown')).toBe('your platform');
  });
});

describe('detectPlatform', () => {
  it('resolves OS and a default arch from a UA', () => {
    expect(detectPlatform(UA.safariMac)).toEqual({ os: 'darwin', arch: 'arm64' });
    expect(detectPlatform(UA.chromeWin)).toEqual({ os: 'windows', arch: 'amd64' });
  });
});
