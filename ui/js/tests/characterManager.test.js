import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const storage = {};

  const localStorage = {
    getItem(key) {
      return storage[key] || null;
    },
    setItem(key, val) {
      storage[key] = val;
    },
    removeItem(key) {
      delete storage[key];
    },
  };

  const ctx = vm.createContext({
    window: windowObj,
    localStorage,
    Math,
    Set,
    JSON,
    Object,
    console,
  });

  const files = [
    "office/tileMap.js",
    "office/pathfinding.js",
    "office/character.js",
    "office/characterManager.js",
  ];
  for (const f of files) {
    vm.runInContext(readFileSync(join(jsDir, f), "utf-8"), ctx);
  }

  return { windowObj, storage };
}

function makeLayout(w, taskCount) {
  return w._officeGenerateLayout(taskCount || 6);
}

describe("CharacterManager", () => {
  it("syncTasks creates one character for a new task", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([{ id: "a", status: "backlog" }]);
    expect(mgr.getCharacterByTaskId("a")).not.toBeNull();
    expect(mgr.getDrawables().length).toBe(1);
  });

  it("second sync with same task updates status, no new character", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([{ id: "a", status: "backlog" }]);
    const ch1 = mgr.getCharacterByTaskId("a");

    mgr.syncTasks([{ id: "a", status: "in_progress" }]);
    const ch2 = mgr.getCharacterByTaskId("a");
    expect(ch2).toBe(ch1); // same object
    expect(mgr.getDrawables().length).toBe(1);
  });

  it("removing a task triggers DESPAWN", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([{ id: "a", status: "backlog" }]);
    mgr.syncTasks([]); // remove all tasks

    const ch = mgr.getCharacterByTaskId("a");
    // Character should be in despawn or already dead
    if (ch) {
      expect(ch.state).toBe("despawn");
    }
  });

  it("assigns sequential desk indices", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([
      { id: "a", status: "backlog" },
      { id: "b", status: "backlog" },
    ]);

    const chA = mgr.getCharacterByTaskId("a");
    const chB = mgr.getCharacterByTaskId("b");
    // They should have different seat positions
    expect(chA.seat).not.toEqual(chB.seat);
  });

  it("localStorage round-trip: assignments survive recreate", () => {
    const { windowObj, storage } = makeContext();
    const layout = makeLayout(windowObj);

    // First manager — assign desk
    const mgr1 = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );
    mgr1.syncTasks([{ id: "a", status: "backlog" }]);
    const seat1 = mgr1.getCharacterByTaskId("a").seat;

    // Second manager — should load from storage
    const mgr2 = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );
    mgr2.syncTasks([{ id: "a", status: "backlog" }]);
    const seat2 = mgr2.getCharacterByTaskId("a").seat;

    expect(seat1).toEqual(seat2);
  });

  it("pruneStaleAssignments removes unknown task IDs", () => {
    const { windowObj, storage } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([
      { id: "a", status: "backlog" },
      { id: "b", status: "backlog" },
    ]);

    mgr.pruneStaleAssignments(["a"]); // "b" is stale

    const data = JSON.parse(storage["wallfacer-office-desks"]);
    expect(data["a"]).toBeDefined();
    expect(data["b"]).toBeUndefined();
  });

  it("characterAt returns correct character for position", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([{ id: "a", status: "backlog" }]);
    const ch = mgr.getCharacterByTaskId("a");
    // Skip spawn
    ch.update(0.6, layout.tileMap);

    const hit = mgr.characterAt(ch.x + 0.5, ch.y + 0.5);
    expect(hit).toBe(ch);
  });

  it("characterAt returns null for empty space", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([{ id: "a", status: "backlog" }]);
    const hit = mgr.characterAt(99, 99);
    expect(hit).toBeNull();
  });

  it("updateAll advances all characters", () => {
    const { windowObj } = makeContext();
    const layout = makeLayout(windowObj);
    const mgr = new windowObj._officeCharacterManager(
      layout.tileMap,
      layout.seats,
    );

    mgr.syncTasks([{ id: "a", status: "backlog" }]);
    const ch = mgr.getCharacterByTaskId("a");
    expect(ch.state).toBe("spawn");

    mgr.updateAll(0.6); // past spawn
    expect(ch.state).toBe("idle");
  });
});
