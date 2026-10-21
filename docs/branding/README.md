# omc branding

The omc identity is a single mark: **a commit node with a signature check**.
A horizontal line (the history), a filled node (the commit), and a check cut
into that node (the signature). It reads as one object at 16 px and as a
diagram at 512 px. The style follows the sibling `goproxy` project icon: a
rounded squircle tile with an indigo→cyan gradient and a near-black glyph.

## Files

| File | Use |
| --- | --- |
| `omc-icon.svg` | Primary app/social icon. Gradient tile, dark glyph. 512 × 512. |
| `omc-mark.svg` | Glyph only, transparent background, gradient fill, check knocked out. Use on any background. |

## Palette

| Token | Value | Role |
| --- | --- | --- |
| Indigo | `#6366f1` | Gradient start (20 % stop) |
| Cyan | `#22d3ee` | Gradient end (100 % stop) |
| Glyph ink | `#04060d` | Tile glyph, terminal-dark contrast |
| Check | `#67e8f9` | Verification accent inside the node |

Gradient is `linear-gradient(135deg, #6366f1 20%, #22d3ee)`, matching the
`goproxy` dashboard logo so the two tools read as one family.

## Geometry

- Canvas 512 × 512, corner radius 128 (25 %), the same optical weight as the
  46 px / 13 px goproxy logo.
- History line at `y=256`, 42 px stroke, round caps, inset 96 px each side.
- Commit node `r=100`, centered.
- Check stroke 24 px, round joins, sitting inside the node.
- Top gloss is a white gradient clipped to the tile, never a separate shape.

## Usage

README head:

```html
<img src="docs/branding/omc-icon.svg" alt="omc icon: a commit node with a check mark" width="104">
```

Favicon or launcher icon:

```html
<link rel="icon" href="docs/branding/omc-mark.svg" type="image/svg+xml">
```

## Rules

- Keep clear space of at least 25 % of the tile width on every side.
- Never stretch, rotate, or re-color the tile; the gradient is the brand.
- On light backgrounds use `omc-mark.svg` or the primary icon; both keep the
  near-black glyph and cyan check contrast.
- Minimum sizes: icon 24 px, mark 16 px.
