# Onboarding and Project History progress polish

Status: frozen handoff for the frontend and backend implementation pass
Visual target: preserve the selected Option 2 two-plane Project Archaeology design, existing Open Sans typography, semantic tokens, Commons mark, and pinned icon set.

## Audit scope and evidence

This is a refinement of the existing first-time Codex sign-in, profile creation, Project History offer, catalog refresh, project selection, and historian-task launch flow. It is not a visual redesign.

Current-run evidence:

1. `/tmp/codex-commons-onboarding-progress-audit/01-sign-in.png` — sign-in entry. Health: visually strong; destination handoff is not automatic.
2. `/tmp/codex-commons-audit-01-profile.png` — first-time profile completion. Health: polished in isolation; visually disconnected from the Project History continuation.
3. `/tmp/codex-commons-audit-02-archaeology-ready.png` — 30-project picker. Health: strong hierarchy and controls; no sort choice and refresh/start lose state clarity during work.
4. Live Project Archaeology storyboard at `project-archaeology-storyboard.html?state=ready` — accessibility tree confirmed search, selected count, candidate metadata, advanced settings, and task-count CTA. Health: semantic foundations are useful; the list needs a compact sort control and continuous progress semantics.

Observed dogfood timing is part of the source of truth: a nine-project start took about 53 seconds before the UI changed screens. All nine then reached `claimed`; one sampled upstream turn later became `interrupted` while Commons still showed `claimed`. A disabled gray button cannot carry these states, and `claimed` cannot be described as running.

## Design principle

Every user action must leave a visible causal trail until the next stable state appears:

`action acknowledged -> current stage named -> real evidence accumulates -> stable result or explicit attention state`

Motion acknowledges state change. Copy explains system truth. Neither motion nor elapsed time implies percentage complete, an ETA, or verified execution.

## 1. Sign-in to first-time onboarding

### Component anatomy

Keep a single modal mounted through:

1. `ready`
2. `connecting`
3. `authorize`
4. `profile`
5. `identity_resolved`
6. `history_offer`
7. `history_capability_check`, only when the capability request takes more than 300 ms

Do not close the auth dialog and open a visually unrelated offer dialog. Once a new Codex profile is saved:

- resolve the Commons endpoint in the existing thread scene;
- hold the identity result for no more than 420 ms;
- replace the dialog body in place with the optional Project History continuation;
- move focus to the new heading only after the user-initiated profile submission succeeds.

Existing users sign in directly and do not receive forced Project History onboarding. `Not now` performs no discovery or import request.

### Exact offer copy

- Eyebrow: `Identity connected`
- Heading: `Bring your Codex work into Commons?`
- Body: `Choose which projects Commons should understand. Nothing is imported until you review it.`
- Primary: `Choose projects`
- Secondary: `Not now`
- Persistent note: `You can return from your account menu.`
- Capability wait: `Checking project history availability…`
- Capability failure: `Project history setup could not be checked. You can try again from your account menu.`

### Transition

- Body exit: opacity 1 to 0 and translateY 0 to -4 px, 90 ms.
- Body entry: opacity 0 to 1 and translateY 8 px to 0, 220 ms, `cubic-bezier(.2,.8,.2,1)`.
- The dialog shell, title position, and footer baseline do not jump.
- Reduced motion: immediate content replacement; focus and live-region updates remain identical.

## 2. Verification destination and pairing-code fallback

`Continue with Codex` is the originating user gesture. In that synchronous click handler, pre-open one blank named window. When the start response supplies `verification_url`, navigate that window to the URL.

This remains device-code authorization. Do not describe it as desktop SSO. The backend contract reports `destination_behavior: manual_code_required`.

If the window is blocked, closed, or cannot be navigated:

- keep the code visible and selected on request;
- elevate `Open Codex sign-in` to the primary link/button;
- show `Your browser could not open it automatically.`

If automatic opening succeeds, retain a quiet `Open sign-in again ↗` link.

The pairing-code card always contains:

- label `One-time code`;
- selectable readonly text input;
- button `Copy code`;
- a single reserved status line so copy feedback never shifts the layout.

Copy outcomes:

- Confirmed browser copy success: `Code copied.`
- Copy failure: focus and select the code, then say `Copy isn’t available here. Press {shortcut} to copy the selected code, or type it in.`

Never claim success because a deprecated browser command merely executed without proof. The code must stay readable at 200% zoom and must not truncate at 320 px.

## 3. Catalog refresh

### Persistent anatomy

Keep the current catalog, selection, search, and summary visible while refreshing. The refresh control retains its dimensions and ordinary contrast; use `aria-busy="true"` rather than collapsing it into a dead gray disabled state. Disable only mutations that would conflict with replacement of the catalog.

Add one compact operation-status line beneath the catalog heading:

`[stage icon] [stage copy] · [elapsed]                         [real count evidence]`

Elapsed time is visual support, not progress. It updates once per second visually, but screen readers hear only stage or count changes.

### Canonical discovery stages and copy

| Backend stage | Visible copy | Evidence when available |
| --- | --- | --- |
| `queued` | `Refresh queued` | none |
| `reading_codex_metadata` | `Reading Codex task metadata` | `{codex_threads_examined} tasks checked` |
| `persisting_catalog` | `Organizing projects` | `{workspaces_grouped} projects found` |
| `ready` | `{candidate_count} projects found` | `Updated just now` |
| `failed` | `Refresh needs attention` | public error text and `Try again` |

Do not show a percentage, synthetic progress bar, rotating tips, or ETA.

The stage icon comes from the existing library. During an active stage its wrapper makes a restrained 12-degree forward/back motion over 900 ms. It is not a continuously rotating spinner. A real stage/count change triggers one 160 ms scale `.96 -> 1` acknowledgement. Reduced motion renders a static icon.

When the refreshed catalog arrives:

- preserve selected project IDs that still exist;
- do not clear the search;
- keep the scroll position when possible;
- announce the new total once;
- briefly tint the status line with semantic green for 600 ms, without moving it.

## 4. Project sorting

Add one compact labeled control after Search and before selected-count actions.

- Visible label: `Sort:` at widths 520 px and above.
- Accessible name: `Sort projects` at every width.
- Options, in this exact order:
  1. `Recent activity`
  2. `Most Codex tasks`
  3. `Name`

`Recent activity` is the default. Apply search first, then sort.

Deterministic ordering:

- Recent activity: non-null `lastActivity.iso` descending; null dates last; then name ascending; then stable project ID.
- Most Codex tasks: `codexThreadCount` descending; recent activity descending; name ascending; stable project ID.
- Name: locale-aware name ascending; then stable project ID.

The sort choice is local presentation state. It does not change selection or create a server write. At 390/320 px and at reflow from 200% text, the sort control occupies a full row below Search and remains text-labeled—never icon-only.

## 5. Starting historian tasks

### Immediate acknowledgement

Within the same event turn as the click:

- retain the blue primary control;
- change its label to `Preparing 0 of {N}`;
- set `aria-busy="true"`;
- freeze configuration inputs while preserving all selected rows and summary facts;
- insert the launch ledger as soon as the server accepts the request.

The backend atomically persists one row per selection before launching. The modal transitions continuously into a ledger; it must not wait for all App Server calls and then jump to a finished-looking screen.

### Ledger anatomy

Header while any task is `preparing` or `starting_codex`:

- Eyebrow: `Codex tasks`
- Heading: `Starting Codex tasks`
- Body: `Commons is creating one ordinary Codex historian task for each selected project.`

Header after every row leaves those two states and no report is ready:

- Heading: `Project history is underway`
- Body: `You can close this window. Commons will preserve each literal task state.`

Summary strip:

`Created {created} of {total} · {accepted} accepted · {attention} need attention · {elapsed}`

Show only counts derived from the backend. Use tabular numerals. The summary is one atomic `aria-live="polite"` region, debounced so simultaneous row updates produce one announcement.

Rows appear immediately in the user’s chosen project order. Each row contains the existing icon wrapper, project name, one literal state sentence, optional secondary evidence, and a quiet provenance disclosure for the task ID after it exists.

### Canonical task-state copy

| State | Primary state copy | Secondary truth |
| --- | --- | --- |
| `preparing` | `Queued in Commons` | `Waiting for a launch slot.` |
| `starting_codex` | `Asking Codex to create the task` | `Commons is durably tracking this request.` |
| `task_created` | `Task created` | `Waiting for Codex to accept the historian.` |
| `claimed` | `Codex accepted this historian` | `Waiting for its first report.` |
| `running` | `Historian is examining project sources` | Show only after a canonical running observation. |
| `report_ready` | `Report ready for Commons` | `Preparing it for your review.` |
| `completed` | `Ready to review` | `Nothing has been imported automatically.` |
| `uncertain` | `Codex may have accepted this task` | `Commons will not retry automatically.` |
| `failed` | `Task needs attention` | Use a bounded public error; never imply a retry is safe. |

An upstream `interrupted` or terminal failure must be persisted/rendered as an attention state rather than left as `claimed`. Do not infer failure from a browser timer. If upstream observation is unavailable, retain the literal claimed state and say when it was last updated. Offer Retry only when the task’s `available_actions` explicitly authorizes it.

### Motion

- Configure-to-ledger transition: same 220 ms body transition as onboarding.
- Row insertion: opacity 0 to 1, translateY 4 px to 0, 180 ms.
- State change: only that row’s icon wrapper scales `.96 -> 1` over 160 ms and receives the semantic tint.
- Completion acknowledgement: 240 ms tint change; no confetti, particles, or layout movement.
- No infinite breathing on completed, failed, uncertain, or claimed rows.

Polling or reconnecting is itself visible: `Checking for task updates…` and then `Updates restored.` If status cannot be refreshed, keep the last known rows and state `Updates paused. Last checked {relative time}.` Do not turn known rows back into skeletons.

## 6. Accessibility and responsive acceptance

### Keyboard and focus

- Dialog focus remains trapped by native dialog behavior and current focus handling.
- Auth to Project History offer: focus the offer heading after successful submit.
- Offer to catalog: focus the catalog heading after the explicit primary action.
- Start to ledger: focus the ledger heading after the explicit start action.
- Background polling never steals focus.
- All focus indicators remain visible in light and dark modes.

### Screen readers

- Use one atomic polite live region for authentication stage.
- Use one atomic polite live region for discovery stage/count changes.
- Use one atomic polite live region for launch summary changes.
- Do not make every changing task row independently live.
- `aria-busy` names the affected region, not the entire page.
- Pure motion and elapsed-second updates are `aria-hidden`.

### Reflow and targets

- Validate 320 px and 390 px widths, 200% text, and both themes.
- No fixed content height for operation rows or status copy.
- Long project names wrap or truncate only where the full accessible label remains available.
- At widths 520 px and below, actionable targets are at least 44 by 44 CSS pixels.
- The task identity remains secondary, selectable, and does not crowd the state sentence.

### Reduced motion

Under `prefers-reduced-motion: reduce`:

- all state transitions are 0 ms;
- active icons remain static;
- no breathing, rotation, translate, or scale animation runs;
- stage names, real counts, elapsed time, focus, and announcements remain present.

## Backend DTO contract

Discovery must be observable through the ordinary session read:

```text
discovery.state
discovery.stage
discovery.started_at
discovery.updated_at
discovery.codex_threads_examined
discovery.workspaces_grouped
```

Task launch must return immediately after atomically reserving selected rows. Each task needs:

```text
launch_id
project_id
state
thread_id? / turn_id?
created_at / updated_at
available_actions
bounded public error?
```

The handoff exposes a count summary with `total`, `preparing`, `starting`, `created`, `claimed`, `running`, `report_ready`, `completed`, `failed`, `uncertain`, and `updated_at`. These counts are preferred over client reconstruction but must equal the bounded task array.

On restart, `preparing` can resume. An in-flight `starting_codex` row becomes `uncertain` and is not automatically retried. A read-only catalog discovery may resume. Upstream interrupted/failed observations become durable attention states.

The installed experimental App Server path may use per-thread dynamic tools and `turn/completed` observations only when both the capability handshake and the Commons handler are present. It must fail closed when unsupported, with a clear bounded unavailable reason. Do not place a grant, credential, or token in the visible task prompt. Map `completed`, `interrupted`, `failed`, and `inProgress` only from the observed App Server event; this is the authority for whether the UI may say running or needs attention.

## Acceptance storyboard

1. First-time user saves a valid profile; the identity resolves and the same modal becomes the optional Project History offer.
2. User chooses projects; the catalog opens, with Recent activity selected and no visual discontinuity.
3. User refreshes; the current catalog remains usable for reading while stage, elapsed time, and real task/project counts update.
4. User chooses Most Codex tasks; list order changes without changing selection.
5. User starts nine projects; the click becomes `Preparing 0 of 9` immediately and nine ledger rows appear after durable acceptance.
6. At most two rows say Codex is being asked to create tasks; created/accepted counts rise from backend state.
7. Claimed rows explicitly say Codex accepted the historian and that Commons is waiting for a report.
8. An interrupted upstream task becomes attention, not a permanently implied running state.
9. User closes the modal; the work continues and reopening restores the same durable ledger.
