# Source and link audit

Audit date: 2026-08-09.

| Source | Reachability | Local copy | Treatment |
|---|---|---|---|
| https://openai.com/brand/ | Public, reviewed | No | Rules cited; marks restricted |
| https://brand.openai.com/ | Authentication required | No | Human review; no bypass |
| https://github.com/openai/apps-sdk-ui | Public, reviewed | Pinned subset | MIT |
| https://openai.github.io/apps-sdk-ui/ | Public | No | Documentation reference |
| https://openai.com/index/introducing-the-codex-app/ | Public, reviewed | No | Media reference-only |
| https://openai.com/codex/get-started/ | Public/search-indexed; may redirect | No | Onboarding reference-only |
| https://openai.com/index/work-with-codex-from-anywhere/ | Public, reviewed | No | Mobile reference-only |
| https://cdn.openai.com/brand/OpenAI-Partnership-Templates-2025.zip | Official download; not fetched | No | Not applicable without approval |

Verified: commit 0f00143c7a639906f1621fe58e1b6be7b5bea46d; package 0.2.2; commit date 2026-05-05; MIT. Local assets are text only: no fonts, executables, archives, images, video, or app extraction.

Known gaps: proprietary Codex desktop tokens/icons/motion are not public; OpenAI Sans and full brand portal require legitimate human access; media has no verified reuse license; pages can change. Re-audit before launch.
