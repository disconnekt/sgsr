# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**sgsr** (Simple Graceful Server Runner) is a Go library that provides a graceful shutdown wrapper for HTTP servers. It handles OS signals (SIGINT, SIGTERM) and ensures servers shut down cleanly with configurable timeouts.

## Architecture

### Core Components

**Config** (`controller.go:14-31`)
- Holds server configuration: `*http.Server`, `*slog.Logger`, and `context.Context`
- Created via `NewConfig(logger, server)` and optionally customized with `WithContext()`
- Server instance must be provided from outside this package

**App** (`controller.go:33-74`)
- Main orchestrator created via `NewApp(config)`
- `Run()` method blocks until shutdown signal received
- Implements dual-timeout strategy:
  - 30s graceful shutdown via `server.Shutdown()`
  - 30s hard exit failsafe via goroutine with `os.Exit(3)`

### Signal Handling Flow

1. App starts listening via `srv.ListenAndServe()` in main goroutine
2. Signal context monitors for SIGINT/SIGTERM
3. On signal received:
   - Logs "trying shut down gracefully"
   - Spawns watchdog goroutine (30s → `os.Exit(3)`)
   - Calls `srv.Shutdown()` with 30s context timeout
4. Returns when either shutdown completes or `http.ErrServerClosed` occurs

## Development Commands

### Testing
```bash
go test ./...           # Run all tests
go test -v ./...        # Run tests with verbose output
go test -cover ./...    # Run tests with coverage
```

### Build and Validation
```bash
go build ./...          # Build the package
go vet ./...            # Run go vet for static analysis
go fmt ./...            # Format code
go mod tidy             # Clean up dependencies
```

### Usage in other projects
This is a library, not a standalone application. Import it in your Go projects:
```go
import "github.com/disconnekt/sgsr"

logger := sgsr.NewLogger() // or provide your own slog.Logger
server := &http.Server{Addr: ":8080", Handler: myHandler}
config := sgsr.NewConfig(logger, server)

// Optional: customize shutdown timeout
config = config.WithShutdownTimeout(60 * time.Second)

// Create the app - this now returns an error if config is invalid
app, err := sgsr.NewApp(config)
if err != nil {
    log.Fatal(err)
}

// Run blocks until shutdown signal received
if err := app.Run(); err != nil {
    log.Fatal(err)
}
```

## Key Improvements

The codebase has been refactored to address security, robustness, and maintainability issues:

- **Error handling**: `Run()` now returns errors instead of panicking; shutdown errors are logged
- **Validation**: `NewApp()` validates that server and logger are non-nil
- **Configurable timeouts**: Default 30s timeout can be customized via `WithShutdownTimeout()`
- **Proper logging**: Uses correct slog key-value pairs (`"server listening", "addr", addr`)
- **No forced exits**: Removed `os.Exit()` watchdog; relies on context timeout for graceful shutdown
- **GoDoc comments**: All public types and functions are documented
- **Named constants**: `DefaultShutdownTimeout` constant instead of magic numbers
- **Test coverage**: Comprehensive unit tests in `controller_test.go`

## Important Constraints

- The server configuration (`*http.Server`) must be fully configured externally with appropriate timeouts
- Shutdown timeout defaults to 30s but should be adjusted based on workload requirements
- Logger writes JSON to stderr by default
