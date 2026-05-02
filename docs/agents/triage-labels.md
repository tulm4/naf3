# Triage Labels

This repo uses the standard 5-label vocabulary for issue state machine.

## Labels

| Label | Purpose |
|-------|---------|
| `needs-triage` | Maintainer needs to evaluate the issue |
| `needs-info` | Waiting on reporter for more information |
| `ready-for-agent` | Fully specified, AFK-ready for an agent |
| `ready-for-human` | Needs human implementation |
| `wontfix` | Will not be actioned |

## State Machine

```
needs-triage → needs-info (if missing info)
            → ready-for-agent (if clear spec)
            → ready-for-human (if needs human judgment)
            → wontfix (if invalid)

ready-for-agent → ready-for-human (if agent blocked)
ready-for-human → done (when implemented)
```

## Usage

The `triage` skill applies these labels by renaming files or updating frontmatter `status` field.
