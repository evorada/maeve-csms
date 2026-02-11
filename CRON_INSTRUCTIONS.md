# MaEVe CSMS — Cron Task Instructions

## Your Mission

Implement OCPP 1.6 message handlers for MaEVe CSMS at `/Users/suda/Projects/Personal/Go/maeve-csms`.
Repository: `git@github.com:evorada/maeve-csms.git` (also accessible via `gaia-charge/maeve-csms`)

## What To Do

1. Read `docs/ocpp16-implementation-plan.md` — find the next unchecked task (top to bottom, module order)
2. **Check which module you're working on** and switch to the correct branch:
   - Module 2: `feature/ocpp16-remote-trigger`
   - Module 3: `feature/ocpp16-smart-charging`
   - Module 4: `feature/ocpp16-firmware-management`
   - Module 5: `feature/ocpp16-local-auth-list`
   - Module 6: `feature/ocpp16-reservation`
   - Module 7: `feature/ocpp16-security-extensions`
3. Create the branch from `main` if it doesn't exist yet: `git checkout -b feature/ocpp16-xxx main`
4. If the branch exists, rebase on main first: `git checkout feature/ocpp16-xxx && git rebase main`
5. Implement **ONE task** fully (store task, or handler task — not multiple)
6. Run the quality pipeline (see below)
7. Commit, push, and check off the task in the plan
8. Commit the plan update to the **same branch**

## Quality Pipeline (MUST PASS before committing)

```bash
cd /Users/suda/Projects/Personal/Go/maeve-csms/manager

# Format
goimports -w .

# Build
go build ./...

# Test
go test -race ./...

# Lint (if golangci-lint is available)
golangci-lint run ./... 2>/dev/null || true
```

ALL must pass with zero errors before you commit.

## Rules

- **One task per session** — e.g., "Task 3.0" or "Task 3.1", never both
- **Store tasks (X.0) come before handler tasks** — never implement a handler without its store
- **All three backends** for store tasks: PostgreSQL, Firestore, In-Memory
- **Follow existing patterns** — look at how existing handlers and stores are structured
- **PostgreSQL uses sqlc** — write SQL queries in `queries/` dir, run sqlc generate
- **Firestore uses collections** — look at existing `manager/store/firestore/*.go`
- **In-memory is a simple map store** — look at `manager/store/inmemory/store.go`
- **Tests are mandatory** — every handler and store method needs tests
- **Match OCPP 1.6 spec exactly** — field names, enums, and behavior per the specification

## Branch Workflow

```bash
# Starting a new module
git checkout main && git pull origin main
git checkout -b feature/ocpp16-xxx

# Continuing existing module
git checkout feature/ocpp16-xxx
git rebase main  # Only if main has new commits

# After implementing
git add -A
git commit -m "feat(ocpp16): Description of what was done"
git push origin feature/ocpp16-xxx
```

## Key Directories

- **Handlers**: `manager/handlers/ocpp16/` — OCPP message handlers
- **OCPP types**: `manager/ocpp/ocpp16/` — Request/response structs
- **Store interfaces**: `manager/store/` — Interface definitions
- **PostgreSQL**: `manager/store/postgres/` — PostgreSQL implementation
  - Migrations: `manager/store/postgres/migrations/`
  - SQL queries: `manager/store/postgres/queries/`
  - sqlc config: `manager/store/postgres/sqlc.yaml`
- **Firestore**: `manager/store/firestore/`
- **In-Memory**: `manager/store/inmemory/`
- **Handler routing**: `manager/handlers/ocpp16/routing.go`
- **Action mapping**: `manager/handlers/router.go`

## Existing Patterns to Follow

### Handler pattern (see `reset.go`, `clear_cache.go`):
```go
type XxxHandler struct {
    store store.Engine
    // ... other dependencies
}

func (h *XxxHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
    // ...
}
```

### Store interface pattern (see `manager/store/cs.go`):
```go
type XxxStore interface {
    Method(ctx context.Context, ...) (result, error)
}
```

### Routing pattern (see `routing.go`):
```go
router.Handle("ActionName", &ActionHandler{store: store})
```

## Session Reporting

End your session with a summary of:
- What task was implemented (task number and description)
- What files were created/modified
- What tests were added
- What was checked off in the plan
- Which branch was used
