# Coverage Improvement Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise root-module test coverage from the measured `60.7%` baseline to `80.0%` or higher without adding coverage-only tests that fail to verify real behavior.

**Architecture:** Work in repeated plan -> plan review -> TDD execution -> verification loops. Start with fully untested deterministic helpers to bank cheap coverage, but move high-yield `hop/conn.go` and `hop/response-writer.go` branch coverage into the mandatory mainline instead of treating it as optional cleanup. If a new test reveals a bug or performance regression, stop and run a dedicated RED -> GREEN -> remeasure loop before resuming the broader coverage plan.

**Tech Stack:** Go 1.25, `testing`, `bytes`, `io`, `net/http`, `bufio`, local packages `internal/bytebufferpool`, `internal/tcplisten`, `transport`, `hop`, `internal/parser`

**Atomic Commit Strategy:** Commit after each verified batch: (1) `test: cover bytebufferpool and helper utilities`, (2) `test: cover transport and hop helper branches`, (3) `test: cover response writer and remaining 80 percent gap`, (4) `fix:` commits only if a RED test reveals a real bug.

---

## Chunk 1: Deterministic 0%-coverage surfaces

### Task 1: Cover `internal/bytebufferpool/bytebuffer.go`

**Files:**
- Create: `internal/bytebufferpool/bytebuffer_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestByteBufferWriteSetAndReset(t *testing.T) {
    var b ByteBuffer
    if _, err := b.WriteString("hello"); err != nil {
        t.Fatalf("WriteString() error = %v", err)
    }
    if err := b.WriteByte('!'); err != nil {
        t.Fatalf("WriteByte() error = %v", err)
    }
    if got := b.String(); got != "hello!" {
        t.Fatalf("String() = %q, want %q", got, "hello!")
    }
    b.Set([]byte("reset"))
    b.Reset()
    if got := b.Len(); got != 0 {
        t.Fatalf("Len() = %d, want 0", got)
    }
}
```

- [ ] **Step 2: Run the RED check**

Run: `rtk go test ./internal/bytebufferpool -run 'TestByteBuffer' -count=1`
Expected: FAIL until the full `ByteBuffer` behavior matrix is present.

- [ ] **Step 3: Add the remaining behavior tests**

Cover:
- `Bytes()` returning the backing slice contents
- `Write([]byte)` append behavior
- `SetString()` replacing contents
- `ReadFrom()` with EOF and non-EOF readers
- `WriteTo()` writing exact bytes to a destination buffer

- [ ] **Step 4: Verify GREEN**

Run: `rtk go test ./internal/bytebufferpool -run 'TestByteBuffer' -count=1`
Expected: PASS.

### Task 2: Cover `internal/bytebufferpool/pool.go`

**Files:**
- Create: `internal/bytebufferpool/pool_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestPoolGetPutResetsReturnedBuffer(t *testing.T) {
    var p Pool
    b := p.Get()
    b.SetString("payload")
    p.Put(b)

    reused := p.Get()
    if reused.Len() != 0 {
        t.Fatalf("Len() = %d, want 0 after reuse", reused.Len())
    }
}
```

- [ ] **Step 2: Run the RED check**

Run: `rtk go test ./internal/bytebufferpool -run 'TestPool|TestIndex|TestCallSizes|TestCalibrate' -count=1`
Expected: FAIL until pool/index/calibration coverage exists.

- [ ] **Step 3: Add the remaining behavior tests**

Cover:
- package-level `Get()` / `Put()` wrappers
- `index()` edge buckets and max clamping
- `callSizes.Len`, `Less`, and `Swap`
- `calibrate()` using synthetic `calls` counts to assert `defaultSize` and `maxSize`

- [ ] **Step 4: Verify GREEN and package coverage**

Run: `rtk go test ./internal/bytebufferpool -cover`
Expected: PASS with package coverage close to complete.

### Task 3: Cover tiny helper files that are currently pure or nearly pure

**Files:**
- Create: `transport/types_test.go`
- Create: `internal/tcplisten/tcplisten_unit_test.go`
- Modify: `hop/composite_buffer_test.go`
- Modify: `hop/parser_test.go`

- [ ] **Step 1: Write failing tests**

Cover:
- `WithReadTimeout`, `WithWriteTimeout`, `WithIdleTimeout`
- `safeIntToUint32()` success, negative, and overflow cases
- `CompositeBuffer.Write()` preserving clone semantics
- `httpVersionString()`, `shouldClose()`, and `shouldCloseValue()`

- [ ] **Step 2: Run the RED check**

Run: `rtk go test ./transport ./internal/tcplisten ./hop -run 'Test(With|SafeIntToUint32|CompositeBufferWrite|HTTPVersionString|ShouldClose)' -count=1`
Expected: FAIL until all helper tests are present.

- [ ] **Step 3: Add the minimal tests only**

Follow the existing table-driven style already used in `internal/parser/*_test.go` and `hop/*_test.go`.

- [ ] **Step 4: Verify GREEN**

Run: `rtk go test ./transport ./internal/tcplisten ./hop -run 'Test(With|SafeIntToUint32|CompositeBufferWrite|HTTPVersionString|ShouldClose)' -count=1`
Expected: PASS.

## Chunk 2: Mandatory high-yield hop and transport branches

### Task 4: Cover deterministic `hop/conn.go` helper and writer-plumbing branches

**Files:**
- Modify: `hop/parser_test.go`

- [ ] **Step 1: Write failing tests**

Cover:
- `hasFoldedPrefix()` positive and negative cases
- `parseHexBytes()` uppercase, lowercase, empty, and invalid hex
- `asciiContainsTokenFold()` comma/space separated token handling
- `connWriteLease.Retain()` and `Release()`
- `connWriteFlusher.Flush()` no-op and buffered flush behavior
- `connWriteFlusher.WriteLease()` buffered vs unbuffered paths
- `firstRequestWindow()` for content-length, partial chunked, and fully terminated chunked bodies

- [ ] **Step 2: Run the RED check**

Run: `rtk go test ./hop -run 'Test(HasFoldedPrefix|ParseHexBytes|ASCIIContainsTokenFold|ConnWrite|FirstRequestWindow)' -count=1`
Expected: FAIL until the new branch tests exist.

- [ ] **Step 3: Add the minimal tests reusing existing doubles**

Reuse `testConn` and related helpers already defined in `hop/parser_test.go`.

- [ ] **Step 4: Verify GREEN and package coverage**

Run: `rtk go test ./hop -cover`
Expected: PASS with `hop/conn.go` materially improved.

### Task 5: Cover currently-uncovered `hop/response-writer.go` default/header paths

**Files:**
- Modify: `hop/response_writer_test.go`

- [ ] **Step 1: Write failing tests**

Cover:
- `Header()` lazily allocating the header map
- `WriteHeader()` updating status without writing output yet
- `writeResponseHeader()` defaulting `Content-Type`, `Date`, and chunked transfer when needed
- `flushPendingBody()` content-length path vs forced chunked path

- [ ] **Step 2: Run the RED check**

Run: `rtk go test ./hop -run 'TestResponseWriter' -count=1`
Expected: FAIL until the missing response-writer branches are covered.

- [ ] **Step 3: Add the minimal tests using existing flush buffer helpers**

Stay in `hop/response_writer_test.go`; do not introduce a second fake writer type unless the current one cannot express the branch.

- [ ] **Step 4: Verify GREEN and package coverage**

Run: `rtk go test ./hop -run 'TestResponseWriter' -count=1 && rtk go test ./hop -cover`
Expected: PASS.

### Task 6: Cover deterministic `transport/netpoll_conn.go` branches without real sockets

**Files:**
- Modify: `transport/netpoll_conn_test.go`

- [ ] **Step 1: Write failing tests**

Cover:
- `Write([]byte{})` returning `(0, nil)` with no flush
- `Peek()` returning nil on empty reader and bytes on populated reader
- `PauseRead()`, `ResumeRead()`, `CompleteRequest()` no-op stability
- `Context()`, `SetContext()`, `LocalAddr()`, `RemoteAddr()`, `Close()`
- `netpollReadLease.Retain()` / `Release()` semantics
- `netpollWriteLease.Bytes()` / `Retain()` / `Release()`

- [ ] **Step 2: Run the RED check**

Run: `rtk go test ./transport -run 'TestNetpollConn|TestNetpoll(Read|Write)Lease' -count=1`
Expected: FAIL until the new branch coverage exists.

- [ ] **Step 3: Add the minimal tests reusing the existing counting connection**

Do not switch to real network sockets for these branches.

- [ ] **Step 4: Verify GREEN and package coverage**

Run: `rtk go test ./transport -cover`
Expected: PASS with `transport/netpoll_conn.go` materially improved.

## Chunk 3: Measure, close the gap, and loop until `>= 80.0%`

### Task 7: Recompute total coverage after Chunks 1 and 2

**Files:**
- Modify: none

- [ ] **Step 1: Measure total coverage**

Run: `tmpfile=$(mktemp "/tmp/http-over-polling-core-cover.XXXXXX") && rtk go test ./... -covermode=atomic -coverprofile="$tmpfile" >/dev/null && go tool cover -func="$tmpfile"`
Expected: PASS and a fresh `total:` line.

- [ ] **Step 2: If total is below `80.0%`, pick the next explicit batch from this ordered list**

Order:
1. `hop/parser.go` remaining helper/table logic adjacent to existing tests
2. `internal/parser/parser.go` `SetUserData()` / `GetUserData()`
3. `internal/parser/engine.go` only if a small, precise branch test can close the remaining gap

### Task 8: Execute one explicit second-wave batch at a time until `total >= 80.0%`

**Files:**
- Modify: whichever file the measurement says is next cheapest

- [ ] **Step 1: Write one failing test for the chosen branch**
- [ ] **Step 2: Run the focused RED check**
- [ ] **Step 3: Add the minimal production fix only if the test exposes a real bug**
- [ ] **Step 4: Re-run the focused test and package coverage**
- [ ] **Step 5: Re-run total coverage**

Stop only when the fresh `go tool cover -func` output shows `total: ... 80.0%` or higher.

## Chunk 4: Final verification

### Task 9: Verify the finished state with fresh evidence

**Files:**
- Modify: none

- [ ] **Step 1: Run the full root-module test suite**

Run: `rtk go test ./... -count=1`
Expected: PASS.

- [ ] **Step 2: Run LSP diagnostics on all changed files**

Expected: zero errors.

- [ ] **Step 3: Capture the final coverage proof**

Run: `tmpfile=$(mktemp "/tmp/http-over-polling-core-cover.XXXXXX") && rtk go test ./... -covermode=atomic -coverprofile="$tmpfile" >/dev/null && go tool cover -func="$tmpfile"`
Expected: fresh `total:` line at or above `80.0%`.

- [ ] **Step 4: Re-run any regression or performance proof added during bugfix loops**

Expected: PASS for each dedicated bugfix/perf verification command before claiming completion.
