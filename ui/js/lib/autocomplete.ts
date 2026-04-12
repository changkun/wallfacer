// --- Trigger-driven textarea autocomplete widget ---
//
// Shared dropdown logic for trigger-based textarea autocomplete (e.g. "@"
// file mentions or "/" slash commands). Callers plug in four callbacks —
// shouldActivate, fetchItems, renderItem, onSelect — and the widget owns
// dropdown creation, positioning, keyboard navigation, and lifecycle.
//
// The widget deliberately does not assume a trigger character or a data
// shape: `shouldActivate` decides when to open (and what the "query" is),
// `fetchItems` produces rows (sync or async), `renderItem` draws each row,
// and `onSelect` mutates the textarea when the user picks one.
//
// Types `AutocompleteMatch`, `AutocompleteOptions`, and `AutocompleteHandle`
// are declared globally in ui/types/globals.d.ts so other script-tag
// consumers can reference them without imports.

function attachAutocomplete<T>(
  textarea: HTMLTextAreaElement | null,
  opts: AutocompleteOptions<T>,
): AutocompleteHandle {
  if (!textarea) {
    return {
      refresh: () => {},
      close: () => {},
      isOpen: () => false,
    };
  }

  const position = opts.position || "below";
  const dropdownClass = opts.dropdownClassName || "mention-dropdown";
  const emptyMessage =
    opts.emptyMessage === undefined ? "No matches" : opts.emptyMessage;
  const triggerOnCursorMove = opts.triggerOnCursorMove !== false;
  const closeOnWindowScroll = opts.closeOnWindowScroll !== false;

  let dropdown: HTMLElement | null = null;
  let selectedIndex = -1;
  let currentMatches: T[] = [];
  // Track rendered item elements so we can update selection styling
  // without touching `dropdown.children` (which eases testing and
  // insulates us from stray DOM children the caller might inject).
  let currentItems: HTMLElement[] = [];
  // Guards stale async loads from overwriting newer renders.
  let renderGeneration = 0;

  function closeDropdown(): void {
    if (dropdown) {
      dropdown.remove();
      dropdown = null;
    }
    selectedIndex = -1;
    currentMatches = [];
    currentItems = [];
  }

  function positionDropdown(): void {
    if (!dropdown || !textarea) return;
    // Guard for test harnesses that don't implement DOM geometry.
    const getRect =
      typeof textarea.getBoundingClientRect === "function"
        ? textarea.getBoundingClientRect.bind(textarea)
        : () => ({ left: 0, top: 0, bottom: 0, width: 0 }) as DOMRect;
    const rect = getRect();
    const wh =
      typeof window !== "undefined" && typeof window.innerHeight === "number"
        ? window.innerHeight
        : 0;
    dropdown.style.position = "fixed";
    dropdown.style.left = rect.left + "px";
    dropdown.style.width = Math.max(320, rect.width) + "px";
    if (position === "above") {
      dropdown.style.bottom = wh - rect.top + 4 + "px";
      dropdown.style.top = "auto";
    } else {
      dropdown.style.top = rect.bottom + 4 + "px";
      dropdown.style.bottom = "auto";
    }
  }

  function scrollSelectedIntoView(): void {
    if (selectedIndex < 0) return;
    const item = currentItems[selectedIndex];
    if (item && typeof item.scrollIntoView === "function") {
      item.scrollIntoView({ block: "nearest" });
    }
  }

  function highlightSelected(): void {
    for (let i = 0; i < currentItems.length; i++) {
      if (i === selectedIndex) {
        currentItems[i].classList.add("mention-item-selected");
      } else {
        currentItems[i].classList.remove("mention-item-selected");
      }
    }
  }

  function selectRow(row: T): void {
    if (!textarea) return;
    const match = opts.shouldActivate(textarea);
    if (!match) {
      closeDropdown();
      return;
    }
    closeDropdown();
    opts.onSelect(row, textarea, match);
  }

  function renderItems(matches: T[]): void {
    if (!dropdown) {
      dropdown = document.createElement("div");
      dropdown.className = dropdownClass;
      document.body.appendChild(dropdown);
    }
    positionDropdown();
    dropdown.innerHTML = "";

    currentItems = [];

    if (matches.length === 0) {
      if (emptyMessage === null) {
        closeDropdown();
        return;
      }
      const empty = document.createElement("div");
      empty.className = "mention-item mention-empty";
      empty.textContent = emptyMessage;
      dropdown.appendChild(empty);
      currentMatches = [];
      selectedIndex = -1;
      return;
    }

    currentMatches = matches;
    if (selectedIndex < 0) selectedIndex = 0;
    selectedIndex = Math.min(selectedIndex, matches.length - 1);

    matches.forEach((row, i) => {
      const item = opts.renderItem(row);
      if (!item.classList.contains("mention-item")) {
        item.classList.add("mention-item");
      }
      if (i === selectedIndex) item.classList.add("mention-item-selected");
      item.addEventListener("mousedown", (e) => {
        e.preventDefault(); // Keep textarea focused.
        selectRow(row);
      });
      dropdown?.appendChild(item);
      currentItems.push(item);
    });
  }

  async function update(): Promise<void> {
    if (!textarea) return;
    const match = opts.shouldActivate(textarea);
    if (!match) {
      closeDropdown();
      return;
    }
    const gen = ++renderGeneration;
    const items = await Promise.resolve(opts.fetchItems(match));
    if (gen !== renderGeneration) return; // Stale load superseded.
    renderItems(items);
  }

  textarea.addEventListener("input", update);

  if (triggerOnCursorMove) {
    textarea.addEventListener("keyup", (e: KeyboardEvent) => {
      if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) {
        update();
      }
    });
    textarea.addEventListener("click", update);
  }

  textarea.addEventListener("keydown", (e: KeyboardEvent) => {
    if (!dropdown) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      selectedIndex =
        currentMatches.length > 0
          ? (selectedIndex + 1) % currentMatches.length
          : 0;
      highlightSelected();
      scrollSelectedIntoView();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selectedIndex =
        currentMatches.length > 0
          ? (selectedIndex - 1 + currentMatches.length) % currentMatches.length
          : 0;
      highlightSelected();
      scrollSelectedIntoView();
    } else if (
      (e.key === "Enter" || e.key === "Tab") &&
      selectedIndex >= 0 &&
      currentMatches.length > 0
    ) {
      e.preventDefault();
      selectRow(currentMatches[selectedIndex]);
    } else if (e.key === "Escape") {
      if (typeof e.stopPropagation === "function") e.stopPropagation();
      closeDropdown();
    }
  });

  textarea.addEventListener("blur", () => {
    // Delay so a mousedown on a dropdown item fires first.
    setTimeout(closeDropdown, 150);
  });

  if (closeOnWindowScroll && typeof window !== "undefined" && window.addEventListener) {
    window.addEventListener("scroll", closeDropdown, { passive: true });
    window.addEventListener("resize", closeDropdown, { passive: true });
  }

  return {
    refresh: () => {
      void update();
    },
    close: closeDropdown,
    isOpen: () => dropdown !== null,
  };
}
