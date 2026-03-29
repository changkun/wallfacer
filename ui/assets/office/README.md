# Pixel Office Assets

The office view uses sprite art from the [LimeZu](https://limezu.itch.io/)
pixel art packs. These assets are licensed for commercial and non-commercial
use but **cannot be redistributed**, so they are not committed to this
repository.

## Required Packs

| Pack | Price | Link |
|------|-------|------|
| Modern Office Revamped [16×16] | $2.50 | https://limezu.itch.io/modernoffice |
| Modern Interiors [16×16] | ~$5–10 | https://limezu.itch.io/moderninteriors |

Total cost: ~$10–15.

## Setup

After purchasing, extract the packs and place sprites as follows:

```
ui/assets/office/
├── characters/
│   ├── char_00.png          # Pre-generated from Modern Interiors Character Generator
│   ├── char_01.png
│   ├── ...
│   └── char_19.png          # 20 characters for variety
├── furniture/
│   ├── desk.png             # From Modern Office pack
│   ├── chair.png
│   ├── pc.png               # Must include on/off animation frames
│   ├── sofa.png
│   ├── plant.png
│   ├── coffee.png
│   ├── whiteboard.png
│   └── bookshelf.png
├── tiles/
│   ├── floor.png            # From Modern Interiors
│   └── wall.png             # Wall auto-tile set
└── effects/
    └── bubbles.png          # Custom speech bubbles (committed to repo)
```

### Characters

Use the **Character Generator** included in Modern Interiors to create 20
diverse character sprite sheets. Each sheet should contain walk animations
in 4 directions (down, left, right, up). Save as `char_00.png` through
`char_19.png`.

### Furniture

Extract individual furniture sprites from the Modern Office pack. Each file
should contain all animation frames (if any) in a horizontal strip:

- `pc.png` — 2 frames: off state, on state (screen lit)
- All others — single frame

### Tiles

- `floor.png` — 4–8 floor tile variants in a horizontal strip
- `wall.png` — Wall auto-tile set for top, bottom, left, right, and corners

## Without Assets

If assets are not present, the office view falls back to colored rectangle
placeholders (development mode) or hides the toggle button entirely.
