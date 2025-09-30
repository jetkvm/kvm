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

### Development Environment

**Recommended:** Development is best done on **Linux** or **macOS**. 

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

3. **Find your JetKVM IP address** (check your router or device screen)

4. **Deploy and test:**
   ```bash
   ./dev_deploy.sh -r 192.168.1.100  # Replace with your device IP
   ```

5. **Open in browser:** `http://192.168.1.100`

That's it! You're now running your own development version of JetKVM.

---

## Common Tasks

### Modify the UI

```bash
cd ui
npm install
./dev_device.sh 192.168.1.100  # Replace with your device IP
```

Now edit files in `ui/src/` and see changes live in your browser!

### Modify the backend

```bash
# Edit Go files (config.go, web.go, etc.)
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
├── config.go           # Settings & configuration
├── web.go              # API endpoints
├── ui/                 # React frontend
│   ├── src/routes/     # Pages (login, settings, etc.)
│   └── src/components/ # UI components
├── internal/           # Internal Go packages
│   ├── native/         # CGO / Native code glue layer
│   ├── native/cgo/     # C files for the native library (HDMI, Touchscreen, etc.)
│   ├── native/eez/     # EEZ Studio Project files (for Touchscreen)
│   ├── hidrpc/         # HIDRPC implementation for HID devices (keyboard, mouse, etc.)
│   ├── logging/        # Logging implementation
│   ├── usbgadget/      # USB gadget 
│   └── websecurity/    # TLS certificate management
└── resource            # netboot iso and other resources
```

**Key files for beginners:**

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

### Touchscreen Changes

Please click the `Build` button in EEZ Studio then run `./dev_deploy.sh -r <YOUR_DEVICE_IP> --skip-ui-build` to deploy the changes to your device. Initial build might take more than 10 minutes as it will also need to fetch and build LVGL and other dependencies.

### Quick Backend Changes

*Best for: API or backend logic changes*

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

### API Testing

```bash
# Test login endpoint
curl -X POST http://<IP>/auth/password-local \
  -H "Content-Type: application/json" \
  -d '{"password": "test123"}'
```

---

## Common Issues & Solutions

### "Build failed" or "Permission denied"

```bash
# Fix permissions
ssh root@<IP> chmod +x /userdata/jetkvm/bin/jetkvm_app_debug

# Clean and rebuild
go clean -modcache
go mod tidy
make build_dev
```

### "Can't connect to device"

```bash
# Check network
ping <IP>

# Check SSH
ssh root@<IP> echo "Connection OK"
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

1. **Backend:** Add API endpoint in `web.go`
2. **Config:** Add settings in `config.go`
3. **Frontend:** Add UI in `ui/src/routes/`
4. **Test:** Deploy and test with `./dev_deploy.sh`

### Code Style

- **Go:** Follow standard Go conventions
- **TypeScript:** Use TypeScript for type safety
- **React:** Keep components small and reusable

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

1. Enable `Developer Mode` on your JetKVM device
2. Add a password on the `Access` tab

```bash
# Access profiling
curl http://api:$JETKVM_PASSWORD@YOUR_DEVICE_IP/developer/pprof/
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


### LVGL Build

We modified the LVGL code a little bit to remove unused fonts and examples.
The patches are generated by

```bash
git diff --cached --diff-filter=d > ../internal/native/cgo/lvgl-minify.patch && \
git diff --name-only --diff-filter=D --cached > ../internal/native/cgo/lvgl-minify.del
```


---

**Happy coding!**

For more information, visit the [JetKVM Documentation](https://jetkvm.com/docs) or join our [Discord Server](https://jetkvm.com/discord).
