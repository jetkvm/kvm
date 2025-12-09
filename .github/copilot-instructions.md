# JetKVM Copilot Instructions

## Repository Overview

**JetKVM** is a high-performance, open-source KVM over IP (Keyboard, Video, Mouse) solution for remote management of computers and servers. The project consists of:

- **Backend**: Go application running on ARM-based JetKVM hardware (ARM7/Rockchip)
- **Frontend**: React/TypeScript UI served by the device and cloud, built with Vite
- **Native Layer**: C code for hardware interaction (HDMI, touchscreen, USB gadgets)

**Repository Size**: ~50MB (excluding node_modules and build artifacts)
**Languages**: Go 1.24.4+, TypeScript/React, C (for native hardware layer)
**Target Platform**: Linux ARM7 (for device), x86_64 (for development/CI)

## Critical Build Requirements

### Prerequisites

- **Go**: 1.24.4 or newer (Go 1.24.10+ recommended)
- **Node.js**: 22.20.0 (REQUIRED - the UI will show warnings with Node 20.x but will still build)
- **npm**: Comes with Node.js
- **Docker**: Required for cross-compilation to ARM unless buildkit is installed locally at `/opt/jetkvm-native-buildkit`

### Environment Setup for Go Tests

**ALWAYS create the `static` directory before running Go tests**:

```bash
mkdir -p static && touch static/.gitkeep
```

The Go code embeds the static directory at build time. Tests will fail with "pattern all:static: no matching files found" if this directory doesn't exist.

## Build and Validation Commands

### Go Backend

**IMPORTANT**: Always run these commands from the repository root, not from subdirectories.

#### Linting
```bash
go vet ./...
```

The project uses golangci-lint in CI with configuration in `.golangci.yml`. Key rules:
- No `fmt.Print*` statements (use logger package instead)
- No `log.Fatal/Panic/Print*` statements (use logger package instead)
- Exceptions: `cmd/main.go` is allowed to use these

#### Testing
```bash
# Create static directory first (REQUIRED)
mkdir -p static && touch static/.gitkeep

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...
```

Tests are located in `*_test.go` files throughout the codebase. Main test directories:
- `internal/confparser`
- `internal/ota`
- `internal/utils`
- `internal/websecure`
- `pkg/nmlite/udhcpc`

#### Building (Development)

The project uses Docker for cross-compilation unless the buildkit is installed locally:

```bash
# Build development version (requires Docker)
make build_dev

# Skip native build if library already exists (faster)
make build_dev SKIP_NATIVE_IF_EXISTS=1

# Skip UI build if static files already exist
make build_dev SKIP_UI_BUILD=1
```

**Build Time**: Initial build with native library compilation can take 10+ minutes. Subsequent builds with `SKIP_NATIVE_IF_EXISTS=1` are much faster (30-60 seconds).

### TypeScript/React UI

**IMPORTANT**: The UI requires Node.js 22.20.0. Node 20.x will show warnings but still work.

#### Install Dependencies
```bash
cd ui
npm ci  # Use ci for reproducible builds, not npm install
```

This will show a warning about Node version mismatch if not on 22.20.0, but will still work.

#### Linting
```bash
cd ui
npm run lint
```

**Note**: The first run will compile i18n files which may show warnings about fetching plugins (expected if offline). This is non-fatal.

Linting uses ESLint with TypeScript, React, and Prettier plugins. Configuration in `ui/eslint.config.cjs`.

#### Building
```bash
cd ui

# Build for device deployment
npm run build:device

# Build for cloud production
npm run build:prod

# Build for cloud staging
npm run build:staging
```

**Build Output**: Files are generated in `../static/` directory (parent of ui/).

**Build Time**: 30-60 seconds for a clean build.

#### Localization (i18n)

The UI uses paraglide-js for compile-time validated localization. All user-facing strings must be localized.

```bash
cd ui

# Validate and compile translations
npm run i18n

# Resort message files
npm run i18n:resort

# Validate translations
npm run i18n:validate

# Compile translations only
npm run i18n:compile

# Machine translate missing strings
npm run i18n:machine-translate
```

**CRITICAL**: Always run `npm run i18n:compile` before linting or building if you've modified translation files in `ui/localization/messages/`.

### Full Build (Backend + Frontend)

```bash
# Build frontend first
make frontend

# Then build backend
make build_dev
```

This is what CI does. The frontend build is automatically integrated into `make build_dev` unless `SKIP_UI_BUILD=1` is set.

## GitHub Actions / CI Pipelines

The repository has several workflows in `.github/workflows/`:

### 1. `build.yml` (Main Build)
Runs on: push to dev/main, PR reviews (when approved), workflow_dispatch

Steps:
1. Checkout code
2. Set up Docker buildx
3. Build Docker image with ARM toolchain
4. Cache CMake build artifacts (`internal/native/cgo/build`)
5. Set up Node.js 22
6. Set up Go 1.25.1+
7. Build frontend (`make frontend`)
8. Build application in Docker (`make build_dev`)
9. Run Go tests (`go test ./... -json`)
10. Build device tests (`make build_dev_test`)
11. Upload artifacts

**Key**: This workflow requires the static directory to exist before running Go tests.

### 2. `golangci-lint.yml`
Runs on: push to Go files, PRs

Steps:
1. Checkout code
2. Install Go (oldstable)
3. **Create empty static directory** (`mkdir -p static && touch static/.gitkeep`)
4. Run golangci-lint v2.1

**CRITICAL**: You must create the static directory or golangci-lint will fail during initialization.

### 3. `ui-lint.yml`
Runs on: push to ui/** files, PRs

Steps:
1. Checkout code
2. Set up Node.js 22
3. Install dependencies (`cd ui && npm ci`)
4. Run lint (`cd ui && npm run lint`)

**Expected**: Node version warnings are expected if CI uses Node 20.x, but build will succeed.

### 4. Other Workflows
- `smoketest.yml`: Automated testing
- `stale-issues.yml`: Issue management

## Project Structure

```
/kvm/
├── .github/
│   ├── workflows/          # CI/CD pipelines
│   ├── ISSUE_TEMPLATE/     # Issue templates
│   └── PULL_REQUEST_TEMPLATE/  # PR templates
├── .golangci.yml           # Go linting configuration
├── Makefile                # Build system
├── main.go                 # Application entry point
├── go.mod, go.sum          # Go dependencies
├── *.go                    # Backend Go code (web.go, config.go, etc.)
├── cmd/
│   └── main.go             # Command entry point
├── internal/               # Internal Go packages
│   ├── confparser/         # Configuration file parsing
│   ├── hidrpc/             # HID device RPC (keyboard/mouse)
│   ├── logging/            # Logging implementation
│   ├── native/             # CGO/C native code glue
│   │   ├── cgo/            # C files for hardware (HDMI, touchscreen)
│   │   │   ├── build/      # CMake build output (cached in CI)
│   │   │   ├── lib/        # Built native libraries
│   │   │   └── include/    # Native headers
│   │   └── eez/            # EEZ Studio touchscreen UI project
│   ├── network/            # Network implementation
│   ├── ota/                # Over-the-air updates
│   ├── usbgadget/          # USB gadget configuration
│   ├── utils/              # Utilities (SSH, etc.)
│   └── websecure/          # TLS certificate management
├── pkg/                    # Public packages
│   ├── myip/               # IP detection
│   └── nmlite/             # Network management
├── scripts/                # Build and deployment scripts
│   ├── build_cgo.sh        # Native library build script
│   ├── dev_deploy.sh       # Deploy to device
│   ├── ci_helper.sh        # CI helper
│   └── build_utils.sh      # Build utilities
├── resource/               # Resources (netboot ISOs)
├── static/                 # Built frontend files (generated)
└── ui/                     # React/TypeScript frontend
    ├── localization/       # i18n translations
    │   ├── messages/       # Translation JSON files (en.json, etc.)
    │   └── paraglide/      # Compiled translations (generated)
    ├── public/             # Static assets (fonts, images)
    ├── src/                # Source code
    │   ├── assets/         # In-page images
    │   ├── components/     # React components
    │   ├── hooks/          # React hooks (stores, RPC)
    │   ├── routes/         # Page components
    │   └── utils/          # Utility functions
    ├── package.json        # npm configuration
    ├── tsconfig.json       # TypeScript configuration
    ├── eslint.config.cjs   # ESLint configuration
    └── vite.config.ts      # Vite build configuration
```

## Key Files by Purpose

### Build Configuration
- `Makefile` - Main build orchestration
- `Dockerfile.build` - Docker image for cross-compilation
- `.golangci.yml` - Go linter config
- `ui/eslint.config.cjs` - TypeScript/React linter config
- `ui/vite.config.ts` - Frontend build config
- `ui/tsconfig.json` - TypeScript compiler config

### Go Code Entry Points
- `main.go` - Main application logic and initialization
- `cmd/main.go` - Command-line entry point
- `web.go` - HTTP server and API endpoints
- `config.go` - Configuration management
- `jsonrpc.go` - JSON-RPC API implementation

### Frontend Entry Points
- `ui/src/main.tsx` - Application entry
- `ui/src/root.tsx` - Root component
- `ui/src/routes/` - Page components

## Common Issues and Solutions

### Issue: Go tests fail with "pattern all:static: no matching files found"
**Solution**: Create the static directory before running tests:
```bash
mkdir -p static && touch static/.gitkeep
go test ./...
```

### Issue: UI build shows Node.js engine warnings
**Solution**: This is expected if using Node 20.x instead of 22.20.0. The build will still succeed. To silence warnings, upgrade to Node 22.20.0.

### Issue: Native build fails or takes too long
**Solution**: Skip native build if the library already exists:
```bash
make build_dev SKIP_NATIVE_IF_EXISTS=1
```

### Issue: golangci-lint fails in CI
**Solution**: Ensure the static directory is created before running the linter, as shown in the CI workflow.

### Issue: UI localization errors
**Solution**: Always compile i18n files before building or linting:
```bash
cd ui && npm run i18n:compile
```

### Issue: Symlink errors with internal/native/cgo/ui
**Solution**: Enable git symlinks and restore the link:
```bash
git config core.symlinks true
git restore internal/native/cgo/ui
```

## Development Workflow

### Making Go Changes
1. Create `static` directory if needed: `mkdir -p static && touch static/.gitkeep`
2. Make code changes
3. Run linter: `go vet ./...`
4. Run tests: `go test ./...`
5. Build: `make build_dev SKIP_UI_BUILD=1` (if UI unchanged)

### Making UI Changes
1. `cd ui`
2. Install dependencies: `npm ci`
3. Make code changes
4. If modifying strings, update translations and run: `npm run i18n`
5. Run linter: `npm run lint`
6. Build: `npm run build:device`
7. Test by viewing files in `../static/`

### Making Changes to Both
1. Follow UI workflow to build frontend first
2. Follow Go workflow, omitting `SKIP_UI_BUILD=1` or running `make frontend` explicitly

## Testing Strategy

### Unit Tests
- Go: Standard `go test` framework
- UI: No test framework currently configured (skip adding UI tests)

### Integration Tests
- Device tests can be built with: `make build_dev_test`
- Creates `device-tests.tar.gz` for testing on actual hardware

### Manual Testing
- Requires a JetKVM device
- Use `./dev_deploy.sh -r <device_ip>` to deploy and test changes

## Important Conventions

### Go Code
- Use logger package, not `fmt.Print*` or `log.*` functions
- Follow standard Go conventions
- Configuration stored in `/userdata/kvm_config.json` on device

### TypeScript/React Code
- **ALL user-facing strings must be localized** using the `m.*` function pattern
- Use TypeScript for type safety
- Follow existing component patterns
- Import order: builtin → external → internal → parent → sibling (enforced by ESLint)

### Commit Messages
- Use clear, descriptive commit messages
- Reference issue numbers when applicable

## Trust These Instructions

These instructions have been validated by:
1. Running `go test ./...` successfully (with static directory)
2. Running `go vet ./...` successfully
3. Running `npm ci` and `npm run lint` in ui/ successfully
4. Reviewing all CI workflows and their requirements
5. Testing build prerequisites and common failure modes

**When working on this repository, trust these instructions first**. Only search for additional information if:
- These instructions are incomplete for your specific task
- You encounter an error not covered here
- You need details about specific code implementation (not build/test)

---

**Last Updated**: 2025-12-09
**Maintained By**: JetKVM Team
