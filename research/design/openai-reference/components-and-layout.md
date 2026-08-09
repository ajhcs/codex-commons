# Components and layout

## Official reusable foundation

Apps SDK UI 0.2.2 includes Alert, Avatar, Badge, Button, Checkbox, CodeBlock, date pickers, EmptyMessage, Image, Indicator, Input, Markdown, Menu, Popover, RadioGroup, SegmentedControl, Select, Slider, Switch, TagInput, links, Textarea, Tooltip, and Transition primitives. The pinned lock resolves Radix UI 1.4.3, React 19.0.0, and Tailwind 4.0.10, all MIT.

Button source includes semantic variants, 22–48 px control sizes, a 2 px focus-visible ring with 2 px offset, disabled/loading states, currentColor icons, fine-pointer hover gating, and 150 ms transitions.

Official Codex docs establish project-organized threads, parallel work, persistent context, diffs, comments, approvals, worktrees, and long-running tasks. They do not publish desktop tokens or APIs. Source: [Introducing the Codex app](https://openai.com/index/introducing-the-codex-app/). Persistent navigation, dominant detail space, list-first selection, restrained surfaces, and focused overlays are **visual inferences**.

## Commons composition

- One persistent left rail: General, Projects, People, compact live presence, human footer.
- No global Review Queue or Background Work; review is contextual and unchanged jobs invisible.
- Thin project tab row, not a second rail.
- Full-width Needs attention and action-changing activity tables.
- Bounded search metadata/snippets; explicit Open for bodies.
- Elevation only for overlays.
- No control before its API, permissions, loading/error/disabled, idempotency, and audit behavior exist.

Every interaction needs default, hover, keyboard focus, pressed/selected, disabled, loading, empty, partial/truncated, stale/disconnected, error, and reduced-motion behavior. Tables need semantic headers, keyboard actions, stable IDs, pagination, and textual state labels.
