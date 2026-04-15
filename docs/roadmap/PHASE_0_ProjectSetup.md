# Phase 0: Project Setup — Skeleton & Foundation

## Overview

Phase 0 xây dựng project skeleton hoàn chỉnh: directory structure, dependency management, CI/CD pipeline, và coding standards. Không có external dependencies (AAA, NRF) trong phase này.

## Modules to Implement

### 1. Project Directory Structure

**Priority:** P0

**Deliverables:**
```
nssAAF/
├── cmd/nssAAF/
│   └── main.go              ← Entry point (EXISTING)
├── internal/
│   ├── api/
│   │   ├── common/          ← Shared utilities
│   │   │   ├── problem.go   ← ProblemDetails (RFC 7807)
│   │   │   ├── validator.go  ← Common validation
│   │   │   ├── headers.go   ← HTTP header constants
│   │   │   └── middleware.go ← Auth, logging middleware
│   │   ├── nssaa/           ← Nnssaaf_NSSAA handler
│   │   │   ├── handler.go
│   │   │   ├── request.go
│   │   │   ├── response.go
│   │   │   ├── router.go
│   │   │   ├── middleware.go
│   │   │   └── handler_test.go
│   │   └── aiw/              ← Nnssaaf_AIW handler
│   │       ├── handler.go
│   │       ├── request.go
│   │       ├── response.go
│   │       ├── router.go
│   │       ├── middleware.go
│   │       └── handler_test.go
│   ├── types/                ← Data types
│   ├── config/               ← Config loading 
│   ├── eap/                  ← Placeholder for Phase 2
│   ├── radius/               ← Placeholder for Phase 2
│   ├── diameter/              ← Placeholder for Phase 2
│   ├── aaa/                  ← Placeholder for Phase 2
│   ├── storage/              ← Placeholder for Phase 3
│   ├── cache/                ← Placeholder for Phase 3
│   ├── resilience/           ← Placeholder for Phase 4
│   ├── auth/                 ← Placeholder for Phase 5
│   ├── crypto/               ← Placeholder for Phase 5
│   ├── nrf/                  ← Placeholder for Phase 6
│   ├── udm/                  ← Placeholder for Phase 6
│   └── amf/                  ← Placeholder for Phase 6
├── configs/
│   ├── development.yaml       ← Dev config (EXISTING)
│   ├── staging.yaml
│   └── production.yaml
├── pkg/                      ← Public packages (future)
├── deployments/
│   ├── helm/
│   ├── kustomize/
│   └── argocd/
├── test/
│   ├── integration/
│   └── e2e/
├── scripts/
│   ├── migrate/
│   └── tools/
├── Makefile                   ← Build targets 
├── go.mod                     ← Go module 
├── go.sum
├── Dockerfile
├── .dockerignore
├── .gitignore
├── .golangci.yml
├── .editorconfig
└── README.md                  ← Project README 
```

### 2. Go Module & Dependencies

**Priority:** P0

**Deliverables:**
- [ ] `go.mod` — Go 1.22+, module path `github.com/operator/nssAAF`
- [ ] Standard library only for Phase 0-1 (no external deps)
- [ ] Add external deps progressively per phase:

| Dep | When Added | Purpose |
|-----|-----------|---------|
| `github.com/google/uuid` | Phase 1 | AuthCtxId generation |
| `gopkg.in/yaml.v3` | Phase 1 | Config loading |
| `github.com/jackc/pgx/v5` | Phase 3 | PostgreSQL driver |
| `github.com/redis/go-redis/v9` | Phase 3 | Redis client |
| `github.com/prometheus/client_golang` | Phase 4 | Metrics |
| `github.com/golang-jwt/jwt/v5` | Phase 5 | JWT validation |
| `github.com/stretchr/testify` | Phase 0 | Unit testing |

### 3. API Common Package (`internal/api/common/`)

**Priority:** P0
**Design Doc:** `docs/design/02_nssaa_api.md` §Common Utilities

**Deliverables:**
- [ ] `problem.go` — ProblemDetails type (RFC 7807)
- [ ] `headers.go` — HTTP header constants
- [ ] `validator.go` — Common validation utilities
- [ ] `middleware.go` — Logging, request ID, auth stubs
- [ ] `common_test.go` — Unit tests

```go
// problem.go — RFC 7807 Problem Details
type ProblemDetails struct {
    Type     string `json:"type,omitempty"`
    Title    string `json:"title,omitempty"`
    Status   int    `json:"status,omitempty"`
    Detail   string `json:"detail,omitempty"`
    Instance string `json:"instance,omitempty"`
    Cause    string `json:"cause,omitempty"`
}

// NewProblem creates a ProblemDetails with required fields.
func NewProblem(status int, cause, detail string) *ProblemDetails

// ValidationProblem creates a ProblemDetails for validation errors.
func ValidationProblem(field, reason string) *ProblemDetails
```

```go
// headers.go
const (
    HeaderContentType  = "Content-Type"
    HeaderAuthorization = "Authorization"
    HeaderXRequestID   = "X-Request-ID"
    HeaderXForwardedFor = "X-Forwarded-For"
    HeaderLocation     = "Location"

    MediaTypeJSON      = "application/json"
    MediaTypeProblemJSON = "application/problem+json"

    OriginNRF = "https://nrf.operator.com"
)
```

```go
// validator.go
func ValidateGPSI(gpsi string) error
func ValidateSUPI(supi string) error
func ValidateSnssai(sst uint8, sd string) error
func ValidateURI(uri string) error
```

```go
// middleware.go
func RequestIDMiddleware(next http.Handler) http.Handler  // Inject X-Request-ID
func LoggingMiddleware(next http.Handler) http.Handler   // Structured logging
func RecoveryMiddleware(next http.Handler) http.Handler // Panic recovery
func CORSMiddleware(next http.Handler) http.Handler    // CORS headers
```

### 4. Project Configuration

**Priority:** P0

**Deliverables:**
- [ ] `configs/development.yaml` — Local dev config (EXISTING)
- [ ] `configs/staging.yaml` — Staging config
- [ ] `configs/production.yaml` — Production config
- [ ] `configs/example.yaml` — Annotated example

```yaml
# configs/production.yaml
server:
  addr: ":8080"
  readTimeout: 10s
  writeTimeout: 30s
  idleTimeout: 120s

database:
  host: "nssAAF-pg-lb.operator.com"
  port: 5432
  name: "nssAAF"
  maxConns: 100
  minConns: 20

redis:
  addrs:
    - "redis-cluster-0:6379"
    - "redis-cluster-1:6379"
    - "redis-cluster-2:6379"
  poolSize: 50

eap:
  maxRounds: 20
  roundTimeout: 30s
  sessionTtl: 5m

aaa:
  responseTimeout: 10s
  maxRetries: 3
  failureThreshold: 5
  recoveryTimeout: 30s

rateLimit:
  perGpsiPerMin: 10
  perAmfPerSec: 1000
  globalPerSec: 100000

logging:
  level: "info"
  format: "json"

metrics:
  enabled: true
  path: "/metrics"
```

### 5. CI/CD Pipeline

**Priority:** P0

**Deliverables:**
- [ ] `.github/workflows/ci.yml` — GitHub Actions CI
- [ ] `.github/workflows/cd.yml` — GitHub Actions CD
- [ ] `.golangci.yml` — golangci-lint config
- [ ] `.editorconfig` — EditorConfig
- [ ] `.gitignore` — Git ignore

```yaml
# .github/workflows/ci.yml
name: CI
on:
  push:
    branches: [main, 'feature/**']
  pull_request:
    branches: [main]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: golangci-lint run ./...

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go test -race -coverprofile=coverage.out ./...
      - uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.out

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go build -o bin/nssAAF ./cmd/nssAAF/

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go install golang.org/x/vuln/cmd/govulncheck@latest
      - run: govulncheck ./...
```

```yaml
# .golangci.yml
version: "2"
linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - bodyclose
    - contextcheck
    - durationcheck
    - errorlint
    - goconst
    - gocyclo
    - gofmt
    - goimports
    - misspell
    - nakedret
    - noctx
    - prealloc
    - revive
    - unconvert
    - unparam

linters-settings:
  revive:
    rules:
      - name: var-naming
      - name: package-comments
  govet:
    enable-all: true
  errorlint:
    errorf: true

run:
  timeout: 5m
  tests: true
```

### 6. Docker & Containerization

**Priority:** P0

**Deliverables:**
- [ ] `Dockerfile` — Multi-stage build
- [ ] `.dockerignore`
- [ ] `Dockerfile.distroless` — Distroless minimal image

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w" \
    -o nssAAF ./cmd/nssAAF/

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/nssAAF .
COPY configs/ ./configs/

EXPOSE 8080 9090 9091

ENTRYPOINT ["/app/nssAAF"]
CMD ["-config", "configs/production.yaml"]
```

### 7. Code Quality Standards

**Priority:** P0

**Deliverables:**
- [ ] `.editorconfig`
- [ ] `CONTRIBUTING.md`
- [ ] `API_STYLE_GUIDE.md`

```yaml
# .editorconfig
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true

[*.go]
indent_style = tab
indent_size = 4

[*.{yaml,json,yml}]
indent_style = space
indent_size = 2

[*.md]
trim_trailing_whitespace = false
```

```bash
# .gitignore
# Binaries
bin/
nssAAF
*.exe

# Coverage
coverage.out
coverage.html

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# Build
dist/

# Secrets (never commit)
*.pem
*.key
.env
secrets.yaml
```

## Implementation Order

```
Week 1 — Days 1-3: Directory structure + go.mod
Week 1 — Days 4-5: internal/api/common/ package
Week 2 — Days 1-2: CI/CD pipeline (.github/workflows)
Week 2 — Days 3-4: golangci-lint + .editorconfig
Week 2 — Day 5: Docker, .gitignore, README
```

## Validation Checklist

- [x] `go build ./...` compiles without errors
- [x] `go test ./...` passes with >0% coverage (baseline)
- [x] `golangci-lint run ./...` passes with no errors
- [x] Docker image builds successfully
- [ ] GitHub Actions CI passes on PR
- [x] All Phase 1-7 placeholder directories created
- [x] `cmd/nssAAF/main.go` wires in `internal/api/common/` middleware
- [x] `configs/production.yaml` exists with full config
- [x] `.editorconfig` enforces code style
- [x] No secrets or credentials in repository
- [x] `make help` shows all available targets

## Spec References

- TS 29.526 §7 — NSSAAF SBI API structure
- TS 29.500 §5 — HTTP/2, TLS requirements
- RFC 7807 — Problem Details for HTTP APIs
- Go Code Review Comments — Style guide
- Effective Go — Official Go style guide
