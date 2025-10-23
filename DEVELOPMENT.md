# JetKVM Development Guide

<div align="center" width="100%">
<img src="https://jetkvm.com/logo-blue.png" align="center" height="28px">

[Discord](https://jetkvm.com/discord) | [Website](https://jetkvm.com) | [Issues](https://github.com/jetkvm/cloud-api/issues) | [Docs](https://jetkvm.com/docs)
[![Twitter](https://img.shields.io/twitter/url/https/twitter.com/jetkvm.svg?style=social&label=Follow%20%40JetKVM)](https://twitter.com/jetkvm)
[![Go Report Card](https://goreportcard.com/badge/github.com/jetkvm/kvm)](https://goreportcard.com/report/github.com/jetkvm/kvm)

</div>

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

```plaintext
/kvm/
├── main.go                   # App entry point
├── config.go                 # Settings & configuration
├── display.go                # Device UI control
├── web.go                    # API endpoints
├── cmd/                      # Command line main
├── internal/                 # Internal Go packages
│   ├── confparser/           # Configuration file implementation
│   ├── hidrpc/               # HIDRPC implementation for HID devices (keyboard, mouse, etc.)
│   ├── logging/              # Logging implementation
│   ├── mdns/                 # mDNS implementation
│   ├── native/               # CGO / Native code glue layer (on-device hardware)
│   │   ├── cgo/              # C files for the native library (HDMI, Touchscreen, etc.)
│   │   └── eez/              # EEZ Studio Project files (for Touchscreen)
│   ├── network/              # Network implementation
│   ├── timesync/             # Time sync/NTP implementation
│   ├── tzdata/               # Timezone data and generation
│   ├── udhcpc/               # DHCP implementation
│   ├── usbgadget/            # USB gadget
│   ├── utils/                # SSH handling
│   └── websecure/            # TLS certificate management
├── resource/                 # netboot iso and other resources
├── scripts/                  # Bash shell scripts for building and deploying
└── static/                   #  (react client build output)
└── ui/                       # React frontend
    ├── localization/         # Client UI localization (i18n)
    │   ├── jetKVM.UI.inlang/ # Settings for inlang
    │   └── messages/         # Messages localized
    ├── public/               # UI website static images and fonts
    └── src/                  # Client React UI
        ├── assets/           # UI in-page images
        ├── components/       # UI components
        ├── hooks/            # Hooks (stores, RPC handling, virtual devices)
        ├── keyboardLayouts/  # Keyboard layout definitions
        ├── paraglide/        #  (localization compiled messages output)
        ├── providers/        # Feature flags
        └── routes/           # Pages (login, settings, etc.)
```

**Key files for beginners:**

- `web.go` - Add new API endpoints here
- `config.go` - Add new settings here
- `ui/src/routes/` - Add new pages here
- `ui/src/components/` - Add new UI components here

---

## Development Modes

### Full Development (Recommended)

#### _Best for: Complete feature development_

```bash
# Deploy everything to your JetKVM device
./dev_deploy.sh -r <YOUR_DEVICE_IP>
```

### Frontend Only

#### _Best for: UI changes without device_

```bash
cd ui
npm install
./dev_device.sh <YOUR_DEVICE_IP>
```

### Touchscreen Changes

Please click the `Build` button in EEZ Studio then run `./dev_deploy.sh -r <YOUR_DEVICE_IP> --skip-ui-build` to deploy the changes to your device. Initial build might take more than 10 minutes as it will also need to fetch and build LVGL and other dependencies.

### Quick Backend Changes

#### _Best for: API or backend logic changes_

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

### "Device UI Fails to Build"

If while trying to build you run into an error message similar to :

```plaintext
In file included from /workspaces/kvm/internal/native/cgo/ctrl.c:15:
/workspaces/kvm/internal/native/cgo/ui_index.h:4:10: fatal error: ui/ui.h: No such file or directory
 #include "ui/ui.h"
          ^~~~~~~~~
compilation terminated.
```

This means that your system didn't create the directory-link to from _./internal/native/cgo/ui_ to ./internal/native/eez/src/ui when the repository was checked out. You can verify this is the case if _./internal/native/cgo/ui_ appears as a plain text file with only the textual contents:

```plaintext
../eez/src/ui
```

If this happens to you need to [enable git creation of symbolic links](https://stackoverflow.com/a/59761201/2076) either globally or for the KVM repository:

```bash
   # Globally enable git to create symlinks
   git config --global core.symlinks true
   git restore internal/native/cgo/ui
```

```bash
   # Enable git to create symlinks only in this project
   git config core.symlinks true
   git restore internal/native/cgo/ui
```

Or if you want to manually create the symlink use:

```bash
   # linux
   cd internal/native/cgo
   rm ui
   ln -s ../eez/src/ui ui
```

```batch
   rem Windows
   cd internal/native/cgo
   del ui
   mklink /d ui ..\eez\src\ui
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
- **Localization:** Ensure all user-facing strings in the frontend are [localized](#localization)

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

### Before submitting

- [ ] Code works on device
- [ ] Tests pass
- [ ] Code follows style guidelines
- [ ] Frontend user-facing strings [localized](#localization)
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

### Localization

The browser/client frontend uses the [paraglide-js](https://inlang.com/m/gerre34r/library-inlang-paraglideJs) plug-in from the [inlang.com](https://inlang.com/) project to allow compile-time validated localization of all user-facing UI strings in the browser/client UI. This includes `title`, `text`, `name`, `description`, `placeholder`, `label`, `aria-label`, _message attributes_ (such as `confirmText`, `unit`, `badge`, `tag`, or `flag`), HTML _element text_ (such as `<h?>`, `<span>`, or `<p>` elements), _notifications messages_, and option _label_ strings, etc.

We **do not** translate the console log messages, CSS class names, theme names, nor the various _value_ strings (e.g. for value/label pair options), nor URL routes.

The localizations are stored in _.json_ files in the `ui/localizations/messages` directory, with one language-per-file using the [ISO 3166-1 alpha-2 country code](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2) (e.g. en for English, de for German, etc.)

#### m-function-matcher

The translations are extracted into language files (e.g. _en.json_ for English) and then paraglide-js compiles them into helpers for use with the [m-function-matcher](https://inlang.com/m/632iow21/plugin-inlang-mFunctionMatcher). An example:

```tsx
<SettingsPageHeader
  title={m.extensions_atx_power_control()}
  description={m.extensions_atx_power_control_description()}
/>
```

#### shakespere plug-in

If you enable the [Sherlock](https://inlang.com/m/r7kp499g/app-inlang-ideExtension) plug-in, the localized text "tooltip" is shown in the VSCode editor after any localized text in the language you've selected for preview. In this image, it's the blue text at the end of the line :

![Showing the translation preview](https://github.com/user-attachments/assets/f6d6dae6-919f-4319-b7bf-500cb1fd458d)

#### Process

##### Localizing a UI

1. Locate a string that is visible to the end user on the client/browser
2. Assign that string a "key" that reflects the logical meaning of the string in snake-case (look at existing localizations for examples), for example if there's a string `This is a test` on the _thing edit page_ it would be "thing_edit_this_is_a_test"

   ```json
   "thing_edit_this_is_a_test": "This is a test",
   ```

3. Add the key and string to the _en.json_ like this:

   - **Note** if the string has replacement parameters (line a user-entered name), the syntax for the localized string has `{ }` around the replacement token (e.g. _This is your name: {name}_). An complex example:

   ```react
   {m.mount_button_showing_results({
      from: indexOfFirstFile + 1,
      to: Math.min(indexOfLastFile, onStorageFiles.length),
      total: onStorageFiles.length
   })}
   ```

4. Save the _en.json_ file and execute `npm run i18n` to resort the language files, validate the translations, and create the m-functions
5. Edit the _.tsx_ file and replace the string with the calls to the new m-function which will be the key-string you chose in snake-case. For example `This is a test` in _thing edit page_ turns into `m.thing_edit_this_is_a_test()`
   - **Note** if the string has a replacement token, supply that to the m-function, for example for the literal `I will call you {name}`, use `m.profile_i_will_call_you({ name: edit.value })`
6. When all your strings are extracted, run `npm run i18n:machine-translate` to get a first-stab at the translations for the other supported languages. Make sure you use an LLM (you can use [aifiesta](https://chat.aifiesta.ai/chat/) to use multiple LLMs) or a [translator](https://translate.google.com) of some form to back-translate each **new** machine-generation in each _language_ to ensure those terms translate reasonably.

### Adding a new language

1. Get the [ISO 3166-1 alpha-2 country code](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2) (for example AT for Austria)
2. Create a new file in the _ui/localization/messages_ directory (example _at.json_)
3. Add the new country code to the _ui/localizations/settings.json_ file in both the `"locales"` and the `"languageTags"` section (inlang and Sherlock aren't exactly current to each other, so we need it in both places).
4. That file also declares the baseLocale/sourceLanguageTag which is `"en"` because this project started out in English. Do NOT change that.
5. Run `npm run i18n:machine-translate` to do an initial pass at localizing all existing messages to the new language.
   - **Note** you will get an error _DB has been closed_, ignore that message, we're not using a database.
   - **Note** you likely will get errors while running this command due to rate limits and such (it uses anonymous Google Translate). Just keep running the command over and over... it'll translate a bunch each time until it says _Machine translate complete_

### Other notes

- Run `npm run i18n:validate` to ensure that language files and settings are well-formed.
- Run `npm run i18n:find-excess` to look for extra keys in other language files that have been deleted from the master-list in _en.json_.
- Run `npm run i18n:find-dupes` to look for multiple keys in _en.json_ that have the same translated value (this is normal)
- Run `npm run i18n:find-unused` to look for keys in _en.json_ that are not referenced in the UI anywhere.
  - **Note** there are a few that are not currently used, only concern yourself with ones you obsoleted.
- Run `npm run i18n:audit` to do all the above checks.
- Using [inlang CLI](https://inlang.com/m/2qj2w8pu/app-inlang-cli) to support the npm commands.
- You can install the [Sherlock VS Code extension](https://marketplace.visualstudio.com/items?itemName=inlang.vs-code-extension) in your devcontainer.

---

**Happy coding!**

For more information, visit the [JetKVM Documentation](https://jetkvm.com/docs) or join our [Discord Server](https://jetkvm.com/discord).
