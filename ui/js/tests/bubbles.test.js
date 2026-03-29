import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const ctx = vm.createContext({ window: windowObj, Math, console });
  vm.runInContext(
    readFileSync(join(jsDir, "office", "bubbles.js"), "utf-8"),
    ctx,
  );
  return windowObj;
}

describe("SpeechBubble", () => {
  const w = makeContext();
  const B = w._officeBubbleTypes;

  it("new SpeechBubble(WAITING) has type 'waiting'", () => {
    const b = new w._officeSpeechBubble(B.WAITING);
    expect(b.type).toBe("waiting");
    expect(b.visible).toBe(true);
  });

  it("update(dt) advances animation frame", () => {
    const b = new w._officeSpeechBubble(B.WAITING);
    // Waiting: 0.4s per frame, 3 frames
    b.update(0.5); // should advance past frame 0
    const info = b.getDrawInfo();
    expect(info.frameIndex).toBe(1);
  });

  it("waiting bubble cycles through 3 frames", () => {
    const b = new w._officeSpeechBubble(B.WAITING);
    const frames = new Set();
    for (let i = 0; i < 15; i++) {
      b.update(0.15);
      frames.add(b.getDrawInfo().frameIndex);
    }
    expect(frames.size).toBe(3);
  });

  it("committing bubble cycles through 4 frames", () => {
    const b = new w._officeSpeechBubble(B.COMMITTING);
    const frames = new Set();
    for (let i = 0; i < 20; i++) {
      b.update(0.1);
      frames.add(b.getDrawInfo().frameIndex);
    }
    expect(frames.size).toBe(4);
  });

  it("failed bubble is static with pulse", () => {
    const b = new w._officeSpeechBubble(B.FAILED);
    b.update(0.0625); // 1/16s → sin(0.0625 * 4 * 2π) = sin(π/2) = 1
    const info = b.getDrawInfo();
    expect(info.frameIndex).toBe(0); // static
    expect(info.pulseScale).not.toBe(1); // pulsing
  });

  it("dismiss starts fade, after 0.2s visible becomes false", () => {
    const b = new w._officeSpeechBubble(B.WAITING);
    expect(b.visible).toBe(true);
    b.dismiss();
    b.update(0.1);
    expect(b.visible).toBe(true); // still fading
    expect(b.getDrawInfo().alpha).toBeLessThan(1);
    b.update(0.15); // total 0.25s > FADE_DURATION
    expect(b.visible).toBe(false);
  });

  it("getDrawInfo returns expected shape", () => {
    const b = new w._officeSpeechBubble(B.WAITING);
    const info = b.getDrawInfo();
    expect(info.type).toBe("waiting");
    expect(typeof info.frameIndex).toBe("number");
    expect(info.visible).toBe(true);
    expect(info.alpha).toBe(1);
    expect(info.pulseScale).toBe(1);
  });
});
