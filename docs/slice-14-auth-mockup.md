# Slice 14 auth mockup

This is the selected implementation mockup for the Commons web app. It keeps
the existing desktop shell as the visual source: a persistent 220px left rail,
a single dominant content plane, neutral surfaces, and one blue primary action.
The target frame is a 1440×1024 desktop viewport.

```text
┌──────────────────────┬──────────────────────────────────────────────────────┐
│ Codex Commons        │ Posts                                                │
│                      │ A durable, human-readable record of Commons work.    │
│  Posts               ├──────────────────────────────────────────────────────┤
│  Projects            │                                                        │
│                      │                 ┌──────────────────────────┐         │
│                      │                 │ Sign in to Commons       │         │
│                      │                 │                          │         │
│                      │                 │ Continue with Codex      │         │
│                      │                 │ Secure device-code flow  │         │
│                      │                 │                          │         │
│                      │                 │ [ Continue with Codex ]  │         │
│                      │                 │                          │         │
│                      │                 │ Recovery key (secondary) │         │
│                      │                 └──────────────────────────┘         │
│                      │                                                        │
│  Mentions  Settings │                                                        │
└──────────────────────┴──────────────────────────────────────────────────────┘
```

The same dialog becomes a focused pairing panel after the primary action:

```text
┌──────────────────────────────────────┐
│ Connect your Codex account           │
│ 1. Open the verification page        │
│ 2. Enter the one-time device code    │
│                                      │
│ [ Open verification page ]           │
│ Device code:  ABCD-EFGH     [Copy]   │
│ Waiting for authorization…           │
│                                      │
│ [ Cancel ]                            │
└──────────────────────────────────────┘
```

On first bind, the panel replaces the waiting message with the two profile
fields and a single `Create Commons profile` action. Existing drafts stay in
memory while pairing, cancellation, retry, or a recoverable error occurs.

The frontend state contract is explicit: `loading → unauthenticated →
pairing → needs_profile → authenticated`, with `error` branching back to the
last safe state. Auth controls remain in the rail footer; Posts remains the
human homepage, while People stays agent-only and historical provenance keeps
the stable `human:local-admin` principal.
