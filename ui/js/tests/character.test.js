import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const ctx = vm.createContext({
    window: windowObj,
    Math,
    Set,
    console,
  });

  // Load dependencies in order
  const files = [
    "office/tileMap.js",
    "office/pathfinding.js",
    "office/character.js",
  ];
  for (const f of files) {
    vm.runInContext(readFileSync(join(jsDir, f), "utf-8"), ctx);
  }

  return windowObj;
}

/** Build a simple open grid. */
function makeGrid(w, width, height) {
  const TileMap = w._officeTileMap;
  const T = w._officeTileTypes;
  const map = new TileMap(width, height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      if (x === 0 || x === width - 1 || y === 0 || y === height - 1) {
        map.setTile(x, y, T.WALL);
      } else {
        map.setTile(x, y, T.FLOOR);
      }
    }
  }
  return map;
}

describe("Character state machine", () => {
  const w = makeContext();
  const S = w._officeCharacterStates;
  const Character = w._officeCharacter;

  it("new character starts in SPAWN state", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    expect(ch.state).toBe(S.SPAWN);
  });

  it("after 0.5s of update, transitions from SPAWN to IDLE", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null);
    expect(ch.state).toBe(S.IDLE);
  });

  it("setTaskStatus('in_progress') on IDLE → WALK_TO_DESK or WORKING", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    // Get past spawn
    ch.update(0.6, null);
    expect(ch.state).toBe(S.IDLE);

    // Move character away from seat so it needs to walk
    ch.x = 1;
    ch.y = 1;
    const map = makeGrid(w, 7, 7);
    ch.setTaskStatus("in_progress", map);
    expect(ch.state).toBe(S.WALK_TO_DESK);
  });

  it("setTaskStatus('in_progress') when already at desk → WORKING", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null);
    // Already at seat position
    ch.setTaskStatus("in_progress", null);
    expect(ch.state).toBe(S.WORKING);
  });

  it("setTaskStatus('waiting') → SPEECH_BUBBLE with amber", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null);
    ch.setTaskStatus("waiting", null);
    expect(ch.state).toBe(S.SPEECH_BUBBLE);
    expect(ch.bubbleType).toBe("amber");
  });

  it("setTaskStatus('failed') → SPEECH_BUBBLE with red", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null);
    ch.setTaskStatus("failed", null);
    expect(ch.state).toBe(S.SPEECH_BUBBLE);
    expect(ch.bubbleType).toBe("red");
  });

  it("setTaskStatus('cancelled') → DESPAWN", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null);
    ch.setTaskStatus("cancelled", null);
    expect(ch.state).toBe(S.DESPAWN);
  });

  it("setTaskStatus('done') → IDLE", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null);
    ch.setTaskStatus("in_progress", null);
    ch.setTaskStatus("done", null);
    expect(ch.state).toBe(S.IDLE);
  });
});

describe("Character walk movement", () => {
  const w = makeContext();
  const Character = w._officeCharacter;
  const S = w._officeCharacterStates;

  it("walks along path and arrives at destination", () => {
    const map = makeGrid(w, 7, 7);
    const ch = new Character("task-1", 0, { x: 4, y: 1, direction: "down" });
    ch.update(0.6, null); // past spawn
    ch.x = 1;
    ch.y = 1;
    ch.setTaskStatus("in_progress", map);
    expect(ch.state).toBe(S.WALK_TO_DESK);

    // Walk at 2 tiles/sec. Distance ~5 tiles → ~2.5s needed.
    for (let i = 0; i < 100; i++) {
      ch.update(0.05, map);
    }
    // Should have arrived at seat (4,1)
    expect(ch.state).toBe(S.WORKING);
    expect(Math.round(ch.x)).toBe(4);
    expect(Math.round(ch.y)).toBe(1);
  });
});

describe("Character animation", () => {
  const w = makeContext();
  const Character = w._officeCharacter;

  it("typing animation alternates frames 0/1", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null); // past spawn
    ch.setTaskStatus("in_progress", null); // at desk → WORKING

    const frames = new Set();
    for (let i = 0; i < 20; i++) {
      ch.update(0.1, null);
      frames.add(ch.getDrawInfo().frameIndex);
    }
    expect(frames.has(0)).toBe(true);
    expect(frames.has(1)).toBe(true);
    expect(frames.size).toBe(6);
  });

  it("walk animation cycles through 4 frames", () => {
    const map = makeGrid(w, 10, 10);
    const ch = new Character("task-1", 0, { x: 8, y: 8, direction: "down" });
    ch.update(0.6, null);
    ch.x = 1;
    ch.y = 1;
    ch.setTaskStatus("in_progress", map);

    const frames = new Set();
    for (let i = 0; i < 40; i++) {
      ch.update(0.1, map);
      if (ch.state === "walk_to_desk") {
        frames.add(ch.getDrawInfo().frameIndex);
      }
    }
    expect(frames.size).toBe(6);
  });
});

describe("Character getDrawInfo", () => {
  const w = makeContext();
  const Character = w._officeCharacter;

  it("returns correct spriteIndex and frame", () => {
    const ch = new Character("task-1", 7, { x: 3, y: 3, direction: "left" });
    const info = ch.getDrawInfo();
    expect(info.spriteIndex).toBe(7);
    expect(info.x).toBe(3);
    expect(info.y).toBe(3);
    expect(info.direction).toBe(w._officeCharacterDirs.LEFT);
    expect(info.state).toBe("spawn");
    expect(typeof info.frameIndex).toBe("number");
  });
});

describe("Character DESPAWN", () => {
  const w = makeContext();
  const Character = w._officeCharacter;

  it("after timer expires, character.dead is true", () => {
    const ch = new Character("task-1", 0, { x: 3, y: 3, direction: "down" });
    ch.update(0.6, null); // past spawn
    ch.setTaskStatus("cancelled", null);
    expect(ch.dead).toBe(false);

    ch.update(0.6, null); // past despawn duration
    expect(ch.dead).toBe(true);
  });
});
