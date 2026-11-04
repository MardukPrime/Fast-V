# Liquid Glass Bottom Dock

FastV uses a Coolapk-inspired liquid-glass bottom dock for quick navigation and proxy actions. It is one of two navigation layouts: sidebar mode keeps the left navigation and hides the dock, while dock mode hides the sidebar and keeps the bottom dock.

## Reference Implementations

The implementation is informed by open-source liquid-glass experiments:

- `rdev/liquid-glass-react`: React liquid-glass component using displacement/refraction layers and pointer-following highlights.
- `shuding/liquid-glass`: Apple Liquid Glass style experiment with layered blur, specular highlights and edge treatment.
- Common glassmorphism CSS patterns: translucent surfaces, `backdrop-filter`, bright inner borders and low-opacity background gradients.

FastV does not add a WebGL or React liquid-glass dependency. The dock is implemented with CSS and one pointer-driven highlight because WebView2 already provides a stable compositor for `backdrop-filter`.

## Visual Layers

The dock combines:

1. A blurred and saturated backdrop.
2. A translucent gradient fill.
3. A bright top specular line.
4. A soft inner border.
5. A pointer-following radial highlight.
6. A deep but restrained outer shadow.

## Selected State

The active bottom-navigation item is not a flat filled button. A separate glass indicator slides between items:

- the indicator is measured from the active button;
- transform and width animate with a spring-like cubic bezier;
- the indicator has its own specular line, inner border and pointer highlight;
- labels and icons stay above the glass layer;
- the active button background is removed so the glass indicator remains visible.

Sidebar active items and settings option cards use the same transparent-glass selection language without the sliding indicator.

## Interaction

The pointer position is written to CSS variables:

```text
--liquid-x
--liquid-y
```

The dock uses those values in a radial gradient so the highlight follows the cursor without rerendering the page.

## Why Not SVG Displacement

SVG displacement looks convincing but has tradeoffs:

- Inconsistent `backdrop-filter: url(...)` support in Chromium/WebView2.
- Higher GPU and compositing cost.
- Text in the dock can become distorted.
- The effect is less predictable at 100%, 125% and 150% DPI.

The selected approach keeps labels crisp while preserving the wet-glass look.

## Responsive Rules

- The dock is only rendered in dock mode; sidebar mode removes the dock entirely.`r`n- The dock remains centered over the main content area.
- The current-node block is hidden first on narrower windows.
- Status text is hidden next.
- Navigation items become flexible and icon-first.

## Performance

- One composited dock layer.
- No per-frame JavaScript animation.
- Pointer updates only write CSS custom properties.
- Parent glass surfaces keep reduced blur to avoid nested compositor cost.