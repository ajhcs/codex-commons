# Visual language

## Official facts

OpenAI describes OpenAI Sans as balancing geometric precision and functionality with a rounded, approachable character. Its brand page describes the Blossom as combining circles (human warmth) and right angles (technical precision). Those statements describe OpenAI identity; the marks are restricted. Source: [OpenAI Design Guidelines](https://openai.com/brand/).

[Apps SDK UI](https://github.com/openai/apps-sdk-ui) documents tokens for color, typography, spacing, sizing, shadows, and surfaces; accessible Radix-based components; dark mode; and responsive utilities. These facts apply to Apps SDK UI, not necessarily Codex desktop.

Exact source facts from package 0.2.2, commit 0f00143:

- neutral light surfaces use white, #f9f9f9, and #f3f3f3, with a dark-aware gray scale;
- semantic families are primary/secondary, info blue, warning orange, caution yellow, danger red, success green, and discovery purple;
- default focus ring is blue; borders use low-alpha neutrals;
- radii are 2, 4, 6, 8, 10, 12, 16, 20, and 24 px plus full;
- spacing uses a 4 px base; hairlines become 0.5 px at 1.5 dppx/150 dpi;
- four restrained elevation geometries run from 1 px offset/2 px blur to 8/16 px.

Exact values: [primitive tokens](assets/official/apps-sdk-ui-0.2.2-0f00143/src/styles/variables-primitive.css) and [semantic tokens](assets/official/apps-sdk-ui-0.2.2-0f00143/src/styles/variables-semantic.css).

## Codex evidence and inference

Official pages document project-organized task threads, context-preserving switching, diff review, comments, worktrees, approvals, and long-running work. Sources: [Codex app announcement](https://openai.com/index/introducing-the-codex-app/) and [onboarding](https://openai.com/codex/get-started/).

Public media visually suggests neutral shells, thin dividers, quiet controls, list-first hierarchy, and a soft blue-purple onboarding atmosphere. This is **visual inference**, not an official token list; media is reference-only.

## Commons decision

Use neutral surfaces and hairlines, one dominant plane, semantic color only for meaning, elevation only for overlays, and a distinct Commons mark. Keep any gradient to onboarding. Color must never be the only state signal; test actual contrast combinations.
