# Quick Task 260504-wwk: improve codebase architecture - Context

**Gathered:** 2026-05-04
**Status:** Ready for planning

<domain>
## Task Boundary

Improve codebase architecture for the 5G NSSAAF Go project. This is a holistic improvement task covering:
1. Extract factory functions from main.go files
2. Implement real metrics for biz/router.go
3. Add domain layer for NssaaStatus state machine
4. Standardize error handling to ProblemDetails
5. Consolidate thin wrapper modules

</domain>

<decisions>
## Implementation Decisions

### Scope depth
- All 5 improvements to be addressed in this quick task

### Main.go refactoring
- Yes, include factory function extraction from main.go files
- Target: cmd/biz/main.go, cmd/http-gateway/main.go, cmd/nrm/main.go
- Extract configuration loading, dependency wiring into dedicated factory packages

### State machine extraction
- Include NssaaStatus domain layer extraction
- Keep it focused: single domain package for state machine logic

### Error handling standardization
- Include in scope
- Audit handlers and add consistent ProblemDetails responses

### Thin wrapper consolidation
- Include in scope
- Review biz/router.go and similar for unnecessary indirection

</decisions>

<specifics>
## Specific Ideas

From architectural analysis:
- Factory package location: `internal/factory/` or per-component factories
- Domain package: `internal/domain/nssaa.go` for NssaaStatus state machine
- Metrics: wire prometheus metrics in biz/router.go
- Error handling: ensure all API handlers return ProblemDetails per TS 29.526

</specifics>

<canonical_refs>
## Canonical References

- TS 29.526 §7.2, §7.3 — API error responses (ProblemDetails)
- TS 29.571 §5.4.4.60-61 — NssaaStatus data type
- Current implementation: internal/eap/, internal/api/nssaa/, internal/api/aiw/

</canonical_refs>
