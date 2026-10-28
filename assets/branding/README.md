# Carbon Panel brand assets

The approved identity pairs the lower-cut block symbol with the technical
capitals wordmark. The opening in the block can suggest a doorway into a
Minecraft world; the wordmark gives the project a practical, technical tone.

## Files

| Asset | Use |
| --- | --- |
| `mark-dark.svg`, `mark-light.svg` | Symbol alone; scalable, transparent SVG. |
| `wordmark-dark.svg`, `wordmark-light.svg` | Lettering alone; scalable, transparent SVG. |
| `horizontal-dark.svg`, `horizontal-light.svg` | Symbol and wordmark side by side. |
| `stacked-dark.svg`, `stacked-light.svg` | Compact symbol and two-line wordmark. |
| `sizes/horizontal-{24,40,64}px-{dark,light}.svg` | Horizontal lockups adjusted for 24, 40, and 64 px symbol heights. |
| `sizes/horizontal-{24,40,64}px-{dark,light}.png` | Transparent 1x raster exports of those lockups. |
| `sizes/mark-{24,40,64}px-{dark,light}.png` | Transparent, square symbol-only raster exports. |
| `approved-logo.vectorcraft` | Editable VectorCraft source and the approved comparison sheet. |

`dark` means dark artwork for a light background (`#202427`); `light` means
light artwork for a dark background (`#f4f4f4`). All SVG text has been
converted to paths, so the exports do not depend on installed fonts. They have
transparent backgrounds. Use the SVGs for arbitrary sizes; the PNGs are useful
when a fixed-size raster image is required.

## Usage and editing

- Keep the proportions and the lower opening intact. Do not stretch the mark,
  fill the opening, add separate details, or recolor it without a brand decision.
- Keep clear space around the logo; use at least the width of the opening as a
  starting point and allow more space where the layout permits.
- Use the mark alone where the complete wordmark would be too small. The
  size-specific lockups adjust text relative to the symbol for readability;
  they are not just uniform resizes of the large horizontal lockup.
- The editable source uses Bahnschrift SemiBold for the approved wordmark and
  Inter for explanatory sheet labels. Those fonts are not included in this
  repository; opening the native source without them may substitute fonts.
  The outlined SVGs preserve the approved shapes without a runtime font.
- The master SVGs and size-specific lockups were exported from VectorCraft.
  Product favicon PNGs are generated from `mark-dark.svg` by
  `python scripts/generate_brand_icons.py` using Pillow 12.3.0. Run the same
  command with `--check` to compare the outputs without writing files.
  `python scripts/sync_brand_assets.py` copies the vector masters to the
  independently built surfaces. `python scripts/check_brand_assets.py` checks
  those copies and key image dimensions using only the Python standard library.
- Preserve the source and review a regenerated export at the intended size
  and on its intended background before replacing a product asset.

The application, landing page and documentation each contain deployable
copies of the approved artwork; update those copies when the master changes.
