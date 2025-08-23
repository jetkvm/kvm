<div align="center">
    <img alt="JetKVM logo" src="https://jetkvm.com/logo-blue.png" height="28">

### Development Guide

[Discord](https://jetkvm.com/discord) | [Website](https://jetkvm.com) | [Issues](https://github.com/jetkvm/cloud-api/issues) | [Docs](https://jetkvm.com/docs)

[![Twitter](https://img.shields.io/twitter/url/https/twitter.com/jetkvm.svg?style=social&label=Follow%20%40JetKVM)](https://twitter.com/jetkvm)

[![Go Report Card](https://goreportcard.com/badge/github.com/jetkvm/kvm)](https://goreportcard.com/report/github.com/jetkvm/kvm)

</div>


# JetKVM Development Guide


Welcome to JetKVM development! This guide will help you get started quickly, whether you're fixing bugs, adding features, or just exploring the codebase.

## Get Started


### Prerequisites
- **A JetKVM device** (for full development)
- **[Go 1.24.4+](https://go.dev/doc/install)** and **[Node.js 22.15.0](https://nodejs.org/en/download/)**
- **[Git](https://git-scm.com/downloads)** for version control
- **[SSH access](https://jetkvm.com/docs/advanced-usage/developing#developer-mode)** to your JetKVM device
- **Audio build dependencies:**
   - **New in this release:** The audio pipeline is now fully in-process using CGO, ALSA, and Opus. You must run the provided scripts in `tools/` to set up the cross-compiler and build static ALSA/Opus libraries for ARM. See below.


### Development Environment

**Recommended:** Development is best done on **Linux** or **macOS**.

#### Apple Silicon (M1/M2/M3) Mac Users

If you are developing on an Apple Silicon Mac, you should use a devcontainer to ensure compatibility with the JetKVM build environment (which targets linux/amd64 and ARM). There are two main options:

- **VS Code Dev Containers**: Open the project in VS Code and use the built-in Dev Containers support. The configuration is in `.devcontainer/devcontainer.json`.
- **Devpod**: [Devpod](https://devpod.sh/) is a fast, open-source tool for running devcontainers anywhere. If you use Devpod, go to **Settings → Experimental → Additional Environmental Variables** and add:
   - `DOCKER_DEFAULT_PLATFORM=linux/amd64`
   This ensures all builds run in the correct architecture.
- **devcontainer CLI**: You can also use the [devcontainer CLI](https://github.com/devcontainers/cli) to launch the devcontainer from the terminal.

This approach ensures compatibility with all shell scripts, build tools, and cross-compilation steps used in the project.

If you're using Windows, we strongly recommend using **WSL (Windows Subsystem for Linux)** for the best development experience:
- [Install WSL on Windows](https://docs.microsoft.com/en-us/windows/wsl/install)
- [WSL Setup Guide](https://docs.microsoft.com/en-us/windows/wsl/setup/environment)

This ensures compatibility with shell scripts and build tools used in the project.


### Project Setup

1. **Clone the repository:**
   ```bash
   git clone https://github.com/jetkvm/kvm.git
   cd kvm
   ```

2. **Check your tools:**
   ```bash
   go version && node --version
   ```

3. **Set up the cross-compiler and audio dependencies:**
   ```bash
   make dev_env
   # This will run tools/setup_rv1106_toolchain.sh and tools/build_audio_deps.sh
   # It will clone the cross-compiler and build ALSA/Opus static libs in $HOME/.jetkvm
   #
   # **Note:** This is required for the new in-process audio pipeline. If you skip this step, audio will not work.
   ```

4. **Find your JetKVM IP address** (check your router or device screen)

5. **Deploy and test:**
   ```bash
   ./dev_deploy.sh -r 192.168.1.100  # Replace with your device IP
   ```

6. **Open in browser:** `http://192.168.1.100`

That's it! You're now running your own development version of JetKVM, **with in-process audio streaming for the first time.**

---

## Common Tasks

### Modify the UI

```bash
cd ui
npm install
./dev_device.sh 192.168.1.100  # Replace with your device IP
```

Now edit files in `ui/src/` and see changes live in your browser!


### Modify the backend (including audio)

```bash
# Edit Go files (config.go, web.go, internal/audio, etc.)
./dev_deploy.sh -r 192.168.1.100 --skip-ui-build
```


### Run tests

```bash
./dev_deploy.sh -r 192.168.1.100 --run-go-tests
```

### View logs

```bash
ssh root@192.168.1.100
tail -f /var/log/jetkvm.log
```

---


## Project Layout

```
/kvm/
├── main.go              # App entry point
├── config.go            # Settings & configuration
├── web.go               # API endpoints
├── ui/                  # React frontend
│   ├── src/routes/      # Pages (login, settings, etc.)
│   └── src/components/  # UI components
├── internal/            # Internal Go packages
│   └── audio/           # In-process audio pipeline (CGO, ALSA, Opus) [NEW]
├── tools/               # Toolchain and audio dependency setup scripts
└── Makefile             # Build and dev automation (see audio targets)
```

**Key files for beginners:**

- `internal/audio/` - [NEW] In-process audio pipeline (CGO, ALSA, Opus)
- `web.go` - Add new API endpoints here
- `config.go` - Add new settings here
- `ui/src/routes/` - Add new pages here
- `ui/src/components/` - Add new UI components here

---

## Development Modes

### Full Development (Recommended)

*Best for: Complete feature development*

```bash
# Deploy everything to your JetKVM device
./dev_deploy.sh -r <YOUR_DEVICE_IP>
```

### Frontend Only

*Best for: UI changes without device*

```bash
cd ui
npm install
./dev_device.sh <YOUR_DEVICE_IP>
```


### Quick Backend Changes

*Best for: API, backend, or audio logic changes (including audio pipeline)*

```bash
# Skip frontend build for faster deployment
./dev_deploy.sh -r <YOUR_DEVICE_IP> --skip-ui-build
```

---

## Debugging Made Easy

### Check if everything is working

```bash
# Test connection to device
ping 192.168.1.100

# Check if JetKVM is running
ssh root@192.168.1.100 ps aux | grep jetkvm
```

### View live logs

```bash
ssh root@192.168.1.100
tail -f /var/log/jetkvm.log
```

### Reset everything (if stuck)

```bash
ssh root@192.168.1.100
rm /userdata/kvm_config.json
systemctl restart jetkvm
```

---

## Testing Your Changes

### Manual Testing

1. Deploy your changes: `./dev_deploy.sh -r <IP>`
2. Open browser: `http://<IP>`
3. Test your feature
4. Check logs: `ssh root@<IP> tail -f /var/log/jetkvm.log`

### Automated Testing

```bash
# Run all tests
./dev_deploy.sh -r <IP> --run-go-tests

# Frontend linting
cd ui && npm run lint
```

### Essential Makefile Targets

The project includes several essential Makefile targets for development environment setup, building, and code quality:

#### Development Environment Setup

```bash
# Set up complete development environment (recommended first step)
make dev_env
# This runs setup_toolchain + build_audio_deps + installs Go tools
# - Clones rv1106-system toolchain to $HOME/.jetkvm/rv1106-system
# - Builds ALSA and Opus static libraries for ARM
# - Installs goimports and other Go development tools

# Set up only the cross-compiler toolchain
make setup_toolchain

# Build only the audio dependencies (requires setup_toolchain)
make build_audio_deps
```

#### Building

```bash
# Build development version with debug symbols
make build_dev
# Builds jetkvm_app with version like 0.4.7-dev20241222
# Requires: make dev_env (for toolchain and audio dependencies)

# Build release version (production)
make build_release
# Builds optimized release version
# Requires: make dev_env and frontend build

# Build test binaries for device testing
make build_dev_test
# Creates device-tests.tar.gz with all test binaries
```

#### Code Quality and Linting

```bash
# Run both Go and UI linting
make lint

# Run both Go and UI linting with auto-fix
make lint-fix

# Run only Go linting
make lint-go

# Run only Go linting with auto-fix
make lint-go-fix

# Run only UI linting
make lint-ui

# Run only UI linting with auto-fix
make lint-ui-fix
```

**Note:** The Go linting targets (`lint-go`, `lint-go-fix`, and the combined `lint`/`lint-fix` targets) require audio dependencies. Run `make dev_env` first if you haven't already.

### Development Deployment Script

The `dev_deploy.sh` script is the primary tool for deploying your development changes to a JetKVM device:

```bash
# Basic deployment (builds and deploys everything)
./dev_deploy.sh -r 192.168.1.100

# Skip UI build for faster backend-only deployment
./dev_deploy.sh -r 192.168.1.100 --skip-ui-build

# Run Go tests on the device after deployment
./dev_deploy.sh -r 192.168.1.100 --run-go-tests

# Deploy with release build and install
./dev_deploy.sh -r 192.168.1.100 -i

# View all available options
./dev_deploy.sh --help
```

**Key features:**
- Automatically builds the Go backend with proper cross-compilation
- Optionally builds the React frontend (unless `--skip-ui-build`)
- Deploys binaries to the device via SSH/SCP
- Restarts the JetKVM service
- Can run tests on the device
- Supports custom SSH user and various deployment options

**Requirements:**
- SSH access to your JetKVM device
- `make dev_env` must be run first (for toolchain and audio dependencies)
- Device IP address or hostname

### API Testing

```bash
# Test login endpoint
curl -X POST http://<IP>/auth/password-local \
  -H "Content-Type: application/json" \
  -d '{"password": "test123"}'
```

---


### Common Issues & Solutions

### "Build failed" or "Permission denied"

```bash
# Fix permissions
ssh root@<IP> chmod +x /userdata/jetkvm/bin/jetkvm_app_debug

# Clean and rebuild
go clean -modcache
go mod tidy
make build_dev
# If you see errors about missing ALSA/Opus or toolchain, run:
make dev_env  # Required for new audio support
```

### "Can't connect to device"

```bash
# Check network
ping <IP>

# Check SSH
ssh root@<IP> echo "Connection OK"
```


### "Audio not working"

```bash
# Make sure you have run:
make dev_env
# If you see errors about ALSA/Opus, check logs and re-run the setup scripts in tools/.
```

### "Frontend not updating"

```bash
# Clear cache and rebuild
cd ui
npm cache clean --force
rm -rf node_modules
npm install
```

---

## Next Steps


### Adding a New Feature

1. **Backend:** Add API endpoint in `web.go` or extend audio in `internal/audio/`
2. **Config:** Add settings in `config.go`
3. **Frontend:** Add UI in `ui/src/routes/`
4. **Test:** Deploy and test with `./dev_deploy.sh`


### Code Style

- **Go:** Follow standard Go conventions
- **TypeScript:** Use TypeScript for type safety
- **React:** Keep components small and reusable
- **Audio/CGO:** Keep C/Go integration minimal, robust, and well-documented. Use zerolog for all logging.

### Environment Variables

```bash
# Enable debug logging
export LOG_TRACE_SCOPES="jetkvm,cloud,websocket,native,jsonrpc"

# Frontend development
export JETKVM_PROXY_URL="ws://<IP>"
```

---

## Need Help?

1. **Check logs first:** `ssh root@<IP> tail -f /var/log/jetkvm.log`
2. **Search issues:** [GitHub Issues](https://github.com/jetkvm/kvm/issues)
3. **Ask on Discord:** [JetKVM Discord](https://jetkvm.com/discord)
4. **Read docs:** [JetKVM Documentation](https://jetkvm.com/docs)

---

## Contributing

### Ready to contribute?

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test thoroughly
5. Submit a pull request

### Before submitting:

- [ ] Code works on device
- [ ] Tests pass
- [ ] Code follows style guidelines
- [ ] Documentation updated (if needed)

---

## Advanced Topics

### Performance Profiling

```bash
# Enable profiling
go build -o bin/jetkvm_app -ldflags="-X main.enableProfiling=true" cmd/main.go

# Access profiling
curl http://<IP>:6060/debug/pprof/
```
### Advanced Environment Variables

```bash
# Enable trace logging (useful for debugging)
export LOG_TRACE_SCOPES="jetkvm,cloud,websocket,native,jsonrpc"

# For frontend development
export JETKVM_PROXY_URL="ws://<JETKVM_IP>"

# Enable SSL in development
export USE_SSL=true
```

### Configuration Management

The application uses a JSON configuration file stored at `/userdata/kvm_config.json`.

#### Adding New Configuration Options

1. **Update the Config struct in `config.go`:**

   ```go
   type Config struct {
       // ... existing fields
       NewFeatureEnabled bool `json:"new_feature_enabled"`
   }
   ```

2. **Update the default configuration:**

   ```go
   var defaultConfig = &Config{
       // ... existing defaults
       NewFeatureEnabled: false,
   }
   ```

3. **Add migration logic if needed for existing installations**


---

**Happy coding!**

For more information, visit the [JetKVM Documentation](https://jetkvm.com/docs) or join our [Discord Server](https://jetkvm.com/discord).
