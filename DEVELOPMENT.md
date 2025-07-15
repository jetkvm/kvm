<div align="center">
    <img alt="JetKVM logo" src="https://jetkvm.com/logo-blue.png" height="28">

### Development Guide

[Discord](https://jetkvm.com/discord) | [Website](https://jetkvm.com) | [Issues](https://github.com/jetkvm/cloud-api/issues) | [Docs](https://jetkvm.com/docs)

[![Twitter](https://img.shields.io/twitter/url/https/twitter.com/jetkvm.svg?style=social&label=Follow%20%40JetKVM)](https://twitter.com/jetkvm)

[![Go Report Card](https://goreportcard.com/badge/github.com/jetkvm/kvm)](https://goreportcard.com/report/github.com/jetkvm/kvm)

</div>

# JetKVM Development Guide

This guide provides comprehensive information for developing, testing, and debugging JetKVM. Whether you're adding new features, fixing bugs, or contributing to the project, this document will help you get started.

## Prerequisites

Before you begin development, ensure you have the following tools installed:

- **Go 1.24.4+** - Backend development
- **Node.js 22.15.0** - Frontend development 
- **npm** - Package management (comes with Node.js)
- **Make** - Build automation
- **Git** - Version control
- **SSH access** - For device deployment and debugging

### Verify Installation

```bash
go version          # Should show Go 1.24.4+
node --version      # Should show v22.15.0
npm --version       # Should show npm version
make --version      # Should show Make version
```

## Project Structure

```
/kvm/
├── main.go                           # Main application entry point
├── config.go                         # Configuration management
├── web.go                           # Web server and API endpoints
├── Makefile                         # Build automation
├── dev_deploy.sh                    # Development deployment script
├── go.mod                           # Go module dependencies
├── ui/                              # Frontend React application
│   ├── src/
│   │   ├── routes/                  # Page components
│   │   ├── components/              # Reusable UI components
│   │   ├── hooks/                   # React hooks
│   │   └── api.ts                   # API client
│   ├── dev_device.sh                # Frontend dev server script
│   └── package.json                 # Frontend dependencies
├── internal/                        # Internal Go packages
│   ├── logging/                     # Logging utilities
│   ├── network/                     # Network configuration
│   └── usbgadget/                   # USB gadget management
├── resource/                        # Static resources
└── bin/                             # Built binaries
```

## Development Approaches

### 1. Full Device Development (Recommended)

This approach requires an actual JetKVM device and provides the most complete development environment.

#### Quick Start

```bash
# Deploy to your JetKVM device
./dev_deploy.sh -r <YOUR_JETKVM_IP>

# Example:
./dev_deploy.sh -r 192.168.1.100
```

#### Development Workflow

```bash
# 1. Build frontend and backend, then deploy
./dev_deploy.sh -r 192.168.1.100

# 2. Skip frontend build for faster iteration (backend changes only)
./dev_deploy.sh -r 192.168.1.100 --skip-ui-build

# 3. Run Go tests on device
./dev_deploy.sh -r 192.168.1.100 --run-go-tests

# 4. Only run tests without deployment
./dev_deploy.sh -r 192.168.1.100 --run-go-tests-only

# 5. Use custom username (default: root)
./dev_deploy.sh -r 192.168.1.100 -u admin
```

#### Manual Build Commands

```bash
# Build frontend for device
make frontend

# Build Go binary for ARM Linux
make build_dev

# Build development version with tests
make build_dev_test

# Build release version
make build_release
```

### 2. Frontend-Only Development

For UI development without requiring a physical device.

#### Setup

```bash
cd ui
npm install
```

#### Development Server

```bash
# Start dev server pointing to a JetKVM device
./dev_device.sh <JETKVM_IP>

# Example:
./dev_device.sh 192.168.1.100

# Alternative npm scripts:
npm run dev <JETKVM_IP>              # Standard development
npm run dev:ssl <JETKVM_IP>          # With SSL enabled
npm run dev:cloud                    # Cloud development mode
```

#### Build Commands

```bash
# Build for device deployment
npm run build:device

# Build for production cloud
npm run build:prod

# Build for staging environment
npm run build:staging

# Lint code
npm run lint

# Fix linting issues automatically
npm run lint:fix
```

## Development Environment Configuration

### Environment Variables

```bash
# Enable trace logging (useful for debugging)
export LOG_TRACE_SCOPES="jetkvm,cloud,websocket,native,jsonrpc"

# For frontend development
export JETKVM_PROXY_URL="ws://<JETKVM_IP>"

# Enable SSL in development
export USE_SSL=true
```

### Go Build Configuration

The project uses cross-compilation for ARM Linux. Build settings are configured in the Makefile:

```makefile
GO_CMD := GOOS=linux GOARCH=arm GOARM=7 go
```

## Testing

### Backend Testing

```bash
# Run Go tests locally
go test ./...

# Build and run tests on device
make build_dev_test

# Deploy with tests to device
./dev_deploy.sh -r <IP> --run-go-tests

# Run only tests (no deployment)
./dev_deploy.sh -r <IP> --run-go-tests-only
```

### Frontend Testing

```bash
cd ui

# Lint code
npm run lint

# Fix linting issues
npm run lint:fix

# Start development server for manual testing
npm run dev <JETKVM_IP>
```

### Integration Testing

1. **Deploy your changes:**
   ```bash
   ./dev_deploy.sh -r <JETKVM_IP>
   ```

2. **Access the web interface:**
   - Navigate to `http://<JETKVM_IP>` in your browser
   - Test your changes in the UI

3. **API testing with curl:**
   ```bash
   # Test authentication endpoints
   curl -X POST http://<IP>/auth/password-local \
     -H "Content-Type: application/json" \
     -d '{"password": "test123"}'
   
   # Test device status
   curl http://<IP>/device/status
   ```

## Debugging

### Application Logs

```bash
# SSH into device
ssh root@<IP>

# View real-time application logs
tail -f /var/log/jetkvm.log

# View systemd logs
journalctl -u jetkvm -f

# View specific log levels
journalctl -u jetkvm -p err
```

### Process Management

```bash
# Check running processes
ps aux | grep jetkvm

# Kill development instance
killall jetkvm_app_debug

# Kill production instance
killall jetkvm_app

# Restart service
systemctl restart jetkvm
```

### Configuration Debugging

```bash
# View current configuration
ssh root@<IP>
cat /userdata/kvm_config.json

# Reset configuration (be careful!)
rm /userdata/kvm_config.json
systemctl restart jetkvm
```

### Network Debugging

```bash
# Check network interfaces
ip addr show

# Test connectivity
ping <TARGET_IP>

# Check open ports
netstat -tulpn | grep jetkvm
```

## Common Development Tasks

### Quick Testing Cycle

```bash
# 1. Make your code changes
# 2. Deploy to device (skip frontend if only backend changes)
./dev_deploy.sh -r <IP> --skip-ui-build

# 3. Test in browser at http://<IP>
# 4. Check logs if needed
ssh root@<IP>
tail -f /var/log/jetkvm.log
```

### Adding New Features

1. **Backend changes:**
   - Modify `config.go` for new configuration options
   - Add API endpoints in `web.go`
   - Update request/response structures

2. **Frontend changes:**
   - Add new routes in `ui/src/routes/`
   - Create reusable components in `ui/src/components/`
   - Update API client in `ui/src/api.ts`

3. **Testing:**
   - Write Go tests for backend functionality
   - Test UI changes with `npm run dev`
   - Deploy and test integration

### Code Style Guidelines

#### Go Code
- Follow standard Go conventions
- Use meaningful variable names
- Add comments for exported functions
- Handle errors appropriately

#### TypeScript/React Code
- Use TypeScript for type safety
- Follow React hooks patterns
- Use consistent naming conventions
- Keep components focused and reusable

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

## Troubleshooting

### Common Issues

#### Build Failures

```bash
# Clear Go module cache
go clean -modcache

# Update dependencies
go mod tidy

# Rebuild from scratch
make clean && make build_dev
```

#### Frontend Issues

```bash
# Clear npm cache
npm cache clean --force

# Remove node_modules and reinstall
rm -rf node_modules
npm install

# Check for TypeScript errors
npm run lint
```

#### Device Connection Issues

```bash
# Check SSH connectivity
ssh root@<IP> echo "Connection OK"

# Verify device is accessible
ping <IP>

# Check if device is running JetKVM
ssh root@<IP> ps aux | grep jetkvm
```

#### Permission Issues

```bash
# Fix binary permissions on device
ssh root@<IP> chmod +x /userdata/jetkvm/bin/jetkvm_app_debug

# Check file ownership
ssh root@<IP> ls -la /userdata/jetkvm/bin/
```

### Getting Help

1. **Check the logs first** - Most issues can be diagnosed from application logs
2. **Search existing issues** - Check [GitHub Issues](https://github.com/jetkvm/kvm/issues)
3. **Ask on Discord** - Join our [Discord Server](https://jetkvm.com/discord)
4. **Read the documentation** - Visit [JetKVM Docs](https://jetkvm.com/docs)

## Contributing

### Before Submitting a Pull Request

1. **Test your changes thoroughly**
2. **Run linting and fix any issues**
3. **Write tests for new functionality**
4. **Update documentation if needed**
5. **Follow the existing code style**

### Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test thoroughly
5. Submit a pull request with a clear description

### Code Review Guidelines

- Be respectful and constructive
- Focus on the code, not the person
- Provide specific, actionable feedback
- Test the changes locally when possible

## Advanced Development Topics

### Cross-Compilation

The project builds for ARM Linux by default. To build for other architectures:

```bash
# Build for x86_64 Linux
GOOS=linux GOARCH=amd64 go build -o bin/jetkvm_app_x64 cmd/main.go

# Build for current platform
go build -o bin/jetkvm_app_native cmd/main.go
```

### Custom Build Tags

```bash
# Build with specific tags
go build -tags "netgo,debug" -o bin/jetkvm_app cmd/main.go
```

### Performance Profiling

```bash
# Enable profiling in development
go build -o bin/jetkvm_app -ldflags="-X main.enableProfiling=true" cmd/main.go

# Access profiling endpoints
curl http://<IP>:6060/debug/pprof/
```

## Release Process

### Development Release

```bash
# Build and upload development release
make dev_release
```

### Production Release

```bash
# Update version in Makefile
VERSION := 0.4.7

# Build and upload production release
make release
```

---

For more information, visit the [JetKVM Documentation](https://jetkvm.com/docs) or join our [Discord Server](https://jetkvm.com/discord).
