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

After purchasing, extract the packs and place **sprite sheets** (not
individual sprites) as follows:

```
ui/assets/office/
├── characters/
│   ├── char_00.png          # Premade_Character_01.png from Modern Interiors
│   ├── char_01.png          # Premade_Character_02.png
│   ├── ...
│   └── char_19.png          # Premade_Character_20.png (20 characters)
├── furniture/
│   ├── office_sheet.png     # Modern_Office_16x16.png (256×848, full furniture sheet)
│   └── room_builder.png     # Room_Builder_16x16.png from Modern Office (office walls)
├── tiles/
│   ├── floor.png            # Room_Builder_Floors_16x16.png (240×640, floor tile set)
│   └── wall.png             # Room_Builder_Walls_16x16.png (512×640, wall auto-tile set)
└── effects/
    └── bubbles.png          # Custom speech bubbles (committed to repo)
```

### Characters

Copy the 20 **Premade Characters** from Modern Interiors
(`2_Characters/Character_Generator/0_Premade_Characters/16x16/`), renaming
`Premade_Character_01.png` → `char_00.png` through
`Premade_Character_20.png` → `char_19.png`.

Each sheet is **896×656 px** (56×41 frames at 16×16 px). The full animation
layout is documented in `Spritesheet_animations_GUIDE.png` included in the
pack. Key animation rows used by the office view:

| Row(s) | Animation | Frames | Used for |
|--------|-----------|--------|----------|
| 0 | Idle (4 dirs) | 1 each | Backlog / done characters standing |
| 1–2 | Walk (4 dirs) | 6 each | Characters moving between tiles |
| 3 | Sit down | varies | Transition to desk |
| 5–6 | Sitting idle | varies | Seated at desk |
| 7–10 | Typing / PC work | varies | `in_progress` task state |

The `SpriteCache` (task 3/13) defines exact frame coordinates for slicing.

### Furniture

The **entire** Modern Office furniture set ships as a single sprite sheet
(`Modern_Office_16x16.png`, 256×848 px, 16×53 tiles). Individual items
(desks, chairs, PCs, monitors, whiteboards, bookshelves, sofas, plants,
coffee machines) are sliced from this sheet at runtime by the `SpriteCache`
using pixel coordinates. **Do not extract individual PNGs** — the renderer
reads directly from the sheet.

### Tiles

Both tile files are full sets from the Modern Interiors Room Builder:

- `floor.png` (240×640) — 5 columns of floor styles, each 3 tiles wide
  with multiple pattern rows. The renderer picks one style.
- `wall.png` (512×640) — Wall auto-tile sets in column groups. Each group
  contains the full set of edges, corners, and fills needed for auto-tiling.

## Without Assets

If assets are not present, the office view falls back to colored rectangle
placeholders (development mode) or hides the toggle button entirely.
