# Chromecast Custom Receiver Setup

This directory contains the custom Google Cast receiver application used for low-latency WebRTC streaming from JetKVM to Chromecast / Google TV devices.

## Prerequisites

- A Google account (personal Gmail works)
- A Chromecast, Chromecast with Google TV, or Google TV Streamer on the same LAN as the JetKVM
- An HTTPS web server to host the receiver HTML (e.g., Caddy, nginx, GitHub Pages)

## Setup Steps

### 1. Register as a Cast Developer

1. Go to the [Google Cast SDK Developer Console](https://cast.google.com/publish)
2. Pay the one-time $5 registration fee
3. Accept the Terms of Service

### 2. Create a Custom Receiver Application

1. Click **Add New Application**
2. Select **Custom Receiver**
3. Fill in:
   - **Name**: `JetKVM` (or any name you prefer)
   - **Receiver Application URL**: The HTTPS URL where you'll host `index.html` (e.g., `https://yourdomain.com/cast-receiver/index.html`)
4. Click **Save**
5. Note the **Application ID** (e.g., `F311D863`)

### 3. Register Your Device for Testing

Unpublished apps only work on registered test devices:

1. In the Cast Developer Console, go to **Cast Receiver Devices**
2. Click **Add New Device**
3. Enter the device's **Cast serial number**:
   - On Google TV Streamer: **Settings > System > About > Cast serial number** (NOT the Android serial)
   - On Chromecast: Check the box or **Google Home app > Device > Settings > Serial number**
4. Wait **15 minutes** for registration to propagate
5. **Hard reboot** the device (unplug power, wait 10 seconds, plug back in)

### 4. Host the Receiver HTML

The `index.html` file must be served over **HTTPS** with a valid TLS certificate. Options:

**Caddy (recommended for self-hosting):**
```
yourdomain.com {
    handle_path /cast-receiver/* {
        root * /path/to/cast-receiver
        file_server
    }
}
```

**GitHub Pages:**
1. Create a repository and push `index.html`
2. Enable GitHub Pages in repository settings
3. Use the resulting `https://username.github.io/repo-name/index.html` URL

### 5. Configure the JetKVM

1. Open the JetKVM web interface
2. Go to **Settings > Video**
3. Set the **Cast Receiver App ID** to the Application ID from step 2
4. Click **Apply**

### 6. Cast

1. In the JetKVM web interface, click the **Cast** button in the toolbar
2. Select your Chromecast / Google TV device from the list
3. The receiver loads on the TV and establishes a direct WebRTC connection to the JetKVM
4. Expected latency: ~300-500ms on LAN

## How It Works

1. The JetKVM connects to the Chromecast via the CASTV2 protocol (TLS on port 8009)
2. It launches the custom receiver app by Application ID
3. The Chromecast loads the receiver HTML from your HTTPS server
4. The JetKVM sends its IP address to the receiver via a custom Cast namespace
5. The receiver opens a WebSocket to the JetKVM for WebRTC signaling
6. A peer-to-peer WebRTC connection is established for H.264 video streaming

## Troubleshooting

- **"Timeout waiting for custom receiver"**: The device is not registered as a test device, or the 15-minute propagation hasn't completed. Hard reboot the device after waiting.
- **Black screen on TV, no video**: Check the receiver's console via `chrome://inspect` in Chrome (device must be on the same network). Look for WebSocket connection errors.
- **Cast button shows no devices**: Ensure the Chromecast is on the same LAN/subnet as the JetKVM. mDNS discovery requires multicast to work.
- **Mixed content errors**: The receiver HTML must be served over HTTPS. The WebSocket connection to the JetKVM uses `ws://` which is allowed from Cast receiver contexts.

## Publishing (Optional)

Once testing is complete, you can publish the app in the Cast Developer Console to make it available on all Chromecast devices without per-device registration.
