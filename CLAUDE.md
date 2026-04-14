# Cloud Storage Freeloader — Development Rules

## Coding Style: Strict Red-Green TDD

This project follows **strict red-green-refactor TDD**. No exceptions.

### The Process

1. **RED**: Write a failing test that defines the expected behavior. Run it. Watch it fail.
2. **GREEN**: Write the **minimum** production code to make that test pass. Nothing more.
3. **REFACTOR**: Clean up while keeping tests green.

### Rules

- **NEVER write production code without a failing test driving it.**
- Write the test file FIRST, run it to confirm it fails, THEN write the implementation.
- Each test should test one behavior. Name tests descriptively.
- Follow the test specifications in `PLAN.md` — the test names and behaviors are defined there.
- Work phase by phase as defined in `PLAN.md`. Do not skip ahead.
- After writing each test, run `go test` to confirm it fails (RED).
- After writing implementation, run `go test` to confirm it passes (GREEN).
- Show the test output at each step so progress is visible.

### Phase Order

Follow the dependency order from PLAN.md:
- Phase 1: Provider Abstraction Layer (interface, memory, profiles, S3, GDrive, OneDrive)
- Phase 2: Erasure Coding Engine (chunker, encoder, pipeline)
- Phase 3: Shard Placement Engine
- Phase 4: Metadata Store
- Phase 5: Virtual File System
- Phase 6+: As defined in PLAN.md

### Provider Isolation (CRITICAL)

- Google Drive & OneDrive: ALL operations scoped to `cloud-storage-freeloader/` folder
- Backblaze B2: Dedicated bucket `freeloader-test-bucket`
- NEVER read, modify, or delete anything outside these boundaries
