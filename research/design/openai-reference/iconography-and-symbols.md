# Iconography and symbols

## Reusable official source

OpenAI's MIT-licensed Apps SDK UI exports SVG React icons. Source measurement at commit 0f00143 found 755 files; all use 1em width/height and currentColor; 730 use a 24×24 viewBox, 12 use 18×18, 5 use 20×20, and 8 use specialist dimensions. Most are filled paths rather than a uniform stroked family.

The full inventory is [icon-catalog.json](icon-catalog.json). Prefer the package instead of copying hundreds of files. Thirty-five generic sources are preserved for inspection.

| Commons meaning | Generic icon |
|---|---|
| General, Projects, People, Search | Home, Folder, Members, Search |
| Disclosure/navigation | ChevronLeft/Right/Down/Up |
| Copy/open/more | Copy, ExternalLink, DotsHorizontal |
| Success/warning/failure/time | CheckCircle, ExclamationMarkCircle, Error, Clock |
| Running/paused/stopped | Play, Pause, Stop, always with text |
| Task/Git/comment | Clipboard, Branch, Commit, Comment |
| Wiki/history | BookOpen, FileDocument, Archive, History |
| Edit/create/filter | Edit, Plus, Filter |

Presence must expose execution and host connectivity as separate text facts. Never infer reachability from a colored dot.

## Restricted

OpenAI-logo and GPT/DALL·E/Sora-specific files were excluded. MIT copyright permission does not grant permission to imply endorsement or use trademarks as Commons branding. Treat OpenAI wordmark, Blossom, ChatGPT, Codex, GPT, and product marks as **reference-only/requires-permission** under the [brand guidelines](https://openai.com/brand/). Create an original Commons symbol not derived from the Blossom.
