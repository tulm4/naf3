# Domain Docs

This project uses a **single-context** layout with GSD (Getting Stuff Done) workflow.

## Location

GSD state files live at `.planning/`:

| File | Purpose |
|------|---------|
| `STATE.md` | Current phase and position in roadmap |
| `PROJECT.md` | Core value proposition, users, and constraints |
| `REQUIREMENTS.md` | Active and validated requirements |
| `ROADMAP.md` | Phase timeline and milestone tracking |
| `phases/NN-CONTEXT.md` | Per-phase captured decisions |
| `phases/NN-PLAN.md` | Per-phase implementation plan |

## Skills That Read This

| Skill | Reads |
|-------|-------|
| `diagnose` | `STATE.md`, `PROJECT.md`, relevant phase plan |
| `tdd` | `REQUIREMENTS.md`, `phases/NN-PLAN.md` |
| `improve-codebase-architecture` | All GSD state files |

## No ADR Directory

This project captures architectural decisions in GSD phase plans and `phases/NN-CONTEXT.md` files rather than separate ADRs.

## Context for New Agents

When starting work, read `.planning/STATE.md` first to understand current phase, then `PROJECT.md` for project goals.
