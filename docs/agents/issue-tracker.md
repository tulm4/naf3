# Agent Skills Configuration

## Issue Tracker

Issues are tracked as local markdown files under `.scratch/`. Each feature/work item gets its own directory.

### Directory Structure

```
.scratch/
  └── <feature-name>/
      ├── issue-001.md
      ├── issue-002.md
      └── ...
```

### Issue Format

```markdown
---
id: issue-001
title: Brief description
status: needs-triage
created: 2026-05-02
---

## Problem

Detailed description of the issue.

## Acceptance Criteria

- [ ] Criterion 1
- [ ] Criterion 2
```

### Commands Used

- `to-issues` / `triage` / `to-prd`: Write files to `.scratch/<feature>/`
- `qa`: Reads from `.scratch/` to file bug reports
