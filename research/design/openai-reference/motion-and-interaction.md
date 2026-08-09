# Motion and interaction

## Exact reusable source values

Apps SDK UI 0.2.2 defines enter cubic-bezier(0.19,1,0.22,1), exit cubic-bezier(0.8,0,0.4,1), snappy exit cubic-bezier(0.65,0,0.4,1), move cubic-bezier(0.65,0,0.35,1), and a 150 ms basic ease.

Animate defaults to 400 ms enter/200 ms exit for opacity-only changes and 500/300 ms with transforms. AnimateLayout defaults to 300 ms enter, exit, and movement, with 100 ms content-enter delay. Popover/menu source commonly uses 350 ms enter and 200 ms exit. These are **implementation defaults**, not a universal OpenAI motion policy.

The source favors opacity/transform/filter for same-space transitions, measured height/width for layout, no initial transition by default, and non-interactive exiting nodes. See pinned [Transition source](assets/official/apps-sdk-ui-0.2.2-0f00143/src/components/Transition).

## Accessibility boundary

Animated scroll honors reduced motion and user interruption; ShimmerText disables animation under reduced motion. Generic Transition source notes broader reduced-motion support as future work. Commons must add a global reduced-motion path.

Commons recipe: 150 ms interaction feedback; about 200–300 ms disclosure; preserve focus and allow interruption; never depend on animation completion; avoid decorative shimmer/per-row streaming motion; show text and elapsed time for long waits; keep onboarding gradient static by default.
