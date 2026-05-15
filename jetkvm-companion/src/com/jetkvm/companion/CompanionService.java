package com.jetkvm.companion;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Presentation;
import android.app.Service;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.PixelFormat;
import android.graphics.drawable.ColorDrawable;
import android.hardware.display.DisplayManager;
import android.hardware.input.InputManager;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.provider.Settings;
import android.util.DisplayMetrics;
import android.util.Log;
import android.view.Display;
import android.view.InputDevice;
import android.view.Gravity;
import android.view.View;
import android.view.Window;
import android.view.WindowManager;
import android.widget.FrameLayout;

import java.io.OutputStream;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.KeyFactory;
import java.security.MessageDigest;
import java.security.PrivateKey;
import java.security.SecureRandom;
import java.security.Signature;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.LinkedHashSet;
import java.util.Locale;
import java.util.UUID;

public class CompanionService extends Service implements InputManager.InputDeviceListener {
    static final String TAG = "JetKVMCompanion";
    static final String ACTION_SCREEN_ON = "com.jetkvm.companion.SCREEN_ON";
    static final String PREFS = "jetkvm_companion";
    static final String KEY_LAUNCH_ON_BOOT = "launch_on_boot";
    static final String KEY_JETKVM_URL = "jetkvm_url";
    static final String KEY_JETKVM_URLS = "jetkvm_urls";
    static final String KEY_JETKVM_PAIRINGS = "jetkvm_pairings";
    static final String DEFAULT_JETKVM_URL = "http://jetkvm.local";
    static final String EXTRA_JETKVM_URL = "jetkvm_url";
    static final String EXTRA_PAIR_REQUEST_ID = "pair_request_id";

    private static final String CHANNEL_ID = "jetkvm-companion";
    private static final int NOTIFICATION_ID = 1001;
    private static final int PAIRING_NOTIFICATION_ID = 1002;
    private static final int PAIRING_LISTEN_PORT = 8787;
    private static final long SCREEN_ON_DISMISS_DELAY_MS = 600;
    private static final long TARGET_REPORT_INTERVAL_MS = 15000;
    private static final long TARGET_LEASE_MS = 120000;
    private static final long TARGET_PRESENTATION_PULSE_MS = 750;
    private static final String JETKVM_INPUT_NAME_TOKEN = "jetkvm";
    private static final String JETKVM_DISPLAY_NAME_TOKEN = "jetkvm";
    private static final String JETKVM_SHORT_DISPLAY_NAME_TOKEN = "jkvm";
    private static final int LINUX_GADGET_VENDOR_ID = 0x1d6b;
    private static final int LINUX_GADGET_PRODUCT_ID = 0x0104;

    private WindowManager windowManager;
    private DisplayManager displayManager;
    private InputManager inputManager;
    private View launchAssistOverlay;
    private TargetPresentation targetPresentation;
    private int targetPresentationDisplayId = -1;
    private final Runnable dismissTargetPresentationRunnable = new Runnable() {
        @Override
        public void run() {
            dismissTargetPresentation("pulseComplete");
        }
    };
    private final Handler handler = new Handler(Looper.getMainLooper());
    private ServerSocket pairingServerSocket;
    private Thread pairingServerThread;
    private boolean jetkvmPeripheralsPresent;
    private boolean hasPairedJetKvmEndpoints;
    private boolean attemptedForCurrentScreen;
    private boolean targetReportScheduled;
    private volatile long targetDeclarationConfirmedUntilMs;
    private JetKvmPeripheralSnapshot currentSnapshot = new JetKvmPeripheralSnapshot();

    private final Runnable targetReportRunnable = new Runnable() {
        @Override
        public void run() {
            targetReportScheduled = false;
            if (!jetkvmPeripheralsPresent) return;
            reportTargetDeclarationAsync();
            scheduleTargetReport();
        }
    };

    private final BroadcastReceiver screenReceiver = new BroadcastReceiver() {
        @Override
        public void onReceive(Context context, Intent intent) {
            String action = intent.getAction();
            Log.i(TAG, "screen receiver action=" + action);
            if (Intent.ACTION_SCREEN_OFF.equals(action)) {
                attemptedForCurrentScreen = false;
                if (jetkvmPeripheralsPresent) {
                    scheduleTargetReport();
                }
                return;
            }
            if (Intent.ACTION_SCREEN_ON.equals(action)) {
                if (!jetkvmPeripheralsPresent) {
                    Log.i(TAG, "screen-on ignored; JetKVM peripherals not present");
                    return;
                }
                scheduleTargetReport();
                if (attemptedForCurrentScreen) {
                    Log.i(TAG, "screen-on ignored; dismiss already attempted for this screen cycle");
                    return;
                }
                attemptedForCurrentScreen = true;
                launchDismissActivity(ACTION_SCREEN_ON);
                handler.postDelayed(new Runnable() {
                    @Override
                    public void run() {
                        if (jetkvmPeripheralsPresent) {
                            pulseTargetPresentation("screen_on");
                        } else {
                            Log.i(TAG, "pending presentation pulse cancelled; JetKVM peripherals removed");
                        }
                    }
                }, SCREEN_ON_DISMISS_DELAY_MS);
            }
        }
    };

    private final DisplayManager.DisplayListener displayListener = new DisplayManager.DisplayListener() {
        @Override
        public void onDisplayAdded(int displayId) {
            updateJetKvmPeripheralState("displayAdded:" + displayId);
            if (jetkvmPeripheralsPresent && isJetKvmExternalDisplay(displayManager.getDisplay(displayId))) {
                pulseTargetPresentation("displayAdded:" + displayId);
            } else {
                Log.i(TAG, "display added ignored " + describeDisplay(displayManager.getDisplay(displayId)));
            }
        }

        @Override
        public void onDisplayChanged(int displayId) {
            updateJetKvmPeripheralState("displayChanged:" + displayId);
            if (jetkvmPeripheralsPresent && isJetKvmExternalDisplay(displayManager.getDisplay(displayId))) {
                Log.i(TAG, "display changed observed " + describeDisplay(displayManager.getDisplay(displayId)));
            } else {
                Log.i(TAG, "display changed ignored " + describeDisplay(displayManager.getDisplay(displayId)));
            }
        }

        @Override
        public void onDisplayRemoved(int displayId) {
            updateJetKvmPeripheralState("displayRemoved:" + displayId);
            if (displayId == targetPresentationDisplayId) {
                dismissTargetPresentation("displayRemoved:" + displayId);
            }
        }
    };

    @Override
    public void onCreate() {
        super.onCreate();
        createChannel();
        startForeground(NOTIFICATION_ID, buildNotification());
        ensureLaunchAssistOverlay();
        displayManager = (DisplayManager) getSystemService(Context.DISPLAY_SERVICE);
        if (displayManager != null) {
            displayManager.registerDisplayListener(displayListener, handler);
        }
        inputManager = (InputManager) getSystemService(Context.INPUT_SERVICE);
        if (inputManager != null) {
            inputManager.registerInputDeviceListener(this, handler);
        }
        updateJetKvmPeripheralState("startup");

        IntentFilter filter = new IntentFilter();
        filter.addAction(Intent.ACTION_SCREEN_OFF);
        filter.addAction(Intent.ACTION_SCREEN_ON);
        registerReceiver(screenReceiver, filter);
        startPairingRequestServer();
        Log.i(TAG, "service onCreate");
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        saveJetKvmUrlFromIntent(intent);
        ensureLaunchAssistOverlay();
        updateJetKvmPeripheralState("startCommand");
        Log.i(TAG, "service onStartCommand");
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        if (inputManager != null) {
            inputManager.unregisterInputDeviceListener(this);
        }
        if (displayManager != null) {
            displayManager.unregisterDisplayListener(displayListener);
        }
        unregisterReceiver(screenReceiver);
        dismissTargetPresentation("destroy");
        removeLaunchAssistOverlay();
        stopPairingRequestServer();
        Log.i(TAG, "service onDestroy");
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    @Override
    public void onInputDeviceAdded(int deviceId) {
        updateJetKvmPeripheralState("inputAdded:" + deviceId);
    }

    @Override
    public void onInputDeviceRemoved(int deviceId) {
        updateJetKvmPeripheralState("inputRemoved:" + deviceId);
    }

    @Override
    public void onInputDeviceChanged(int deviceId) {
        updateJetKvmPeripheralState("inputChanged:" + deviceId);
    }

    static JetKvmPeripheralSnapshot getJetKvmPeripheralSnapshot() {
        return getJetKvmPeripheralSnapshot(null);
    }

    static JetKvmPeripheralSnapshot getJetKvmPeripheralSnapshot(DisplayManager displayManager) {
        return getJetKvmPeripheralSnapshot(displayManager, null);
    }

    static JetKvmPeripheralSnapshot getJetKvmPeripheralSnapshot(DisplayManager displayManager, String[] expectedIdentityTokens) {
        JetKvmPeripheralSnapshot snapshot = new JetKvmPeripheralSnapshot();
        int[] ids = InputDevice.getDeviceIds();
        for (int id : ids) {
            InputDevice device = InputDevice.getDevice(id);
            if (device == null) {
                continue;
            }

            JetKvmInputIdentity identity = JetKvmInputIdentity.from(device, expectedIdentityTokens);
            if (!identity.isJetKvm) continue;

            snapshot.deviceCount++;
            if (identity.usesLinuxGadgetIds) {
                snapshot.linuxGadgetIdCount++;
            }

            int sources = device.getSources();
            if ((sources & InputDevice.SOURCE_KEYBOARD) == InputDevice.SOURCE_KEYBOARD) {
                snapshot.keyboard = true;
            }
            if ((sources & InputDevice.SOURCE_TOUCHSCREEN) == InputDevice.SOURCE_TOUCHSCREEN) {
                snapshot.touchscreen = true;
            }
            if ((sources & InputDevice.SOURCE_MOUSE) == InputDevice.SOURCE_MOUSE
                    || (sources & InputDevice.SOURCE_MOUSE_RELATIVE) == InputDevice.SOURCE_MOUSE_RELATIVE) {
                snapshot.pointer = true;
            }
        }
        if (displayManager != null) {
            Display[] displays = displayManager.getDisplays();
            for (Display display : displays) {
                if (isJetKvmExternalDisplayStatic(display, expectedIdentityTokens)) {
                    snapshot.monitor = true;
                    snapshot.displayCount++;
                }
            }
        }
        snapshot.present = snapshot.keyboard || snapshot.touchscreen || snapshot.pointer || snapshot.monitor;
        return snapshot;
    }

    private void updateJetKvmPeripheralState(String reason) {
        SharedPreferences prefs = getCompanionPreferences(this);
        boolean hasPairedEndpoints = getPairedJetKvmUrls(prefs).length > 0;
        boolean pairingStateChanged = hasPairedEndpoints != hasPairedJetKvmEndpoints;
        hasPairedJetKvmEndpoints = hasPairedEndpoints;
        JetKvmPeripheralSnapshot snapshot = getJetKvmPeripheralSnapshot(
            displayManager,
            getPairedJetKvmIdentityTokens(prefs)
        );
        snapshot.present = snapshot.present && hasPairedEndpoints;
        currentSnapshot = snapshot;
        if (snapshot.present != jetkvmPeripheralsPresent) {
            jetkvmPeripheralsPresent = snapshot.present;
            attemptedForCurrentScreen = false;
            targetDeclarationConfirmedUntilMs = 0;
            if (!jetkvmPeripheralsPresent) {
                targetReportScheduled = false;
                handler.removeCallbacks(targetReportRunnable);
                reportTargetDisconnectAsync();
                dismissTargetPresentation(reason);
            } else {
                reportTargetDeclarationAsync();
                scheduleTargetReport();
                if (!isServiceLifecycleReason(reason)) {
                    pulseTargetPresentation(reason);
                }
            }
            Log.i(TAG, "JetKVM peripheral state changed reason=" + reason + " " + snapshot);
            updateNotification();
        } else if (pairingStateChanged) {
            updateNotification();
        } else if ("startup".equals(reason) || "startCommand".equals(reason)) {
            Log.i(TAG, "JetKVM peripheral snapshot reason=" + reason + " " + snapshot);
            if (jetkvmPeripheralsPresent) {
                reportTargetDeclarationAsync();
                scheduleTargetReport();
            }
        }
    }

    private boolean isServiceLifecycleReason(String reason) {
        return "startup".equals(reason) || "startCommand".equals(reason);
    }

    private void scheduleTargetReport() {
        if (targetReportScheduled) return;
        targetReportScheduled = true;
        handler.postDelayed(targetReportRunnable, TARGET_REPORT_INTERVAL_MS);
    }

    private void reportTargetDeclarationAsync() {
        final String[] jetkvmUrls = getPairedJetKvmUrls(getCompanionPreferences(this));
        final DisplayMetrics metrics = getResources().getDisplayMetrics();
        final JetKvmPeripheralSnapshot snapshot = currentSnapshot;
        final int width = Math.min(metrics.widthPixels, metrics.heightPixels);
        final int height = Math.max(metrics.widthPixels, metrics.heightPixels);
        if (width <= 0 || height <= 0) return;

        new Thread(new Runnable() {
            @Override
            public void run() {
                for (String jetkvmUrl : jetkvmUrls) {
                    postTargetDeclaration(jetkvmUrl, true, width, height, snapshot);
                }
            }
        }, "JetKVM-target-report").start();
    }

    private void reportTargetDisconnectAsync() {
        final String[] jetkvmUrls = getPairedJetKvmUrls(getCompanionPreferences(this));
        new Thread(new Runnable() {
            @Override
            public void run() {
                for (String jetkvmUrl : jetkvmUrls) {
                    postTargetDeclaration(jetkvmUrl, false, 0, 0, currentSnapshot);
                }
            }
        }, "JetKVM-target-disconnect").start();
    }

    private void saveJetKvmUrlFromIntent(Intent intent) {
        if (intent == null || !intent.hasExtra(EXTRA_JETKVM_URL)) return;

        String value = intent.getStringExtra(EXTRA_JETKVM_URL);
        if (value == null) return;

        value = value.trim();
        if (value.length() == 0) value = DEFAULT_JETKVM_URL;
        boolean saved = addJetKvmUrl(getCompanionPreferences(this), value);
        Log.i(TAG, "JetKVM URL updated from intent saved=" + saved + " url=" + value);
    }

    private void postTargetDeclaration(String baseUrl, boolean connected, int width, int height, JetKvmPeripheralSnapshot snapshot) {
        HttpURLConnection conn = null;
        try {
            String trimmedBaseUrl = baseUrl == null ? "" : baseUrl.trim();
            if (trimmedBaseUrl.length() == 0) trimmedBaseUrl = DEFAULT_JETKVM_URL;
            while (trimmedBaseUrl.endsWith("/")) {
                trimmedBaseUrl = trimmedBaseUrl.substring(0, trimmedBaseUrl.length() - 1);
            }

            URL url = new URL(trimmedBaseUrl + "/companion/target");
            SharedPreferences prefs = getCompanionPreferences(this);
            CompanionPairing pairing = getPairing(prefs, trimmedBaseUrl);
            if (pairing == null) {
                Log.i(TAG, "target declaration skipped unpaired url=" + trimmedBaseUrl);
                return;
            }
            String identityToken = pairing.identityToken;
            if (identityToken.length() == 0) {
                Log.i(TAG, "target declaration skipped missing identity url=" + trimmedBaseUrl);
                return;
            }
            String body;
            if (connected) {
                body = String.format(
                    Locale.US,
                    "{\"state\":\"connected\",\"jetkvm_usb_identity\":\"%s\",\"target_type\":\"android\",\"preferred_mouse_mode\":\"digitizer\",\"display_width\":%d,\"display_height\":%d,\"display_aspect\":%.8f,\"lease_ms\":%d,\"evidence\":[%s]}",
                    identityToken,
                    width,
                    height,
                    (double) width / (double) height,
                    TARGET_LEASE_MS,
                    snapshot.toJsonEvidence()
                );
            } else {
                body = String.format(
                    Locale.US,
                    "{\"state\":\"disconnected\",\"jetkvm_usb_identity\":\"%s\",\"target_type\":\"android\"}",
                    identityToken
                );
            }
            byte[] bodyBytes = body.getBytes(StandardCharsets.UTF_8);

            conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(3000);
            conn.setReadTimeout(3000);
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.setRequestProperty("Content-Type", "application/json; charset=utf-8");
            applyCompanionSignatureHeaders(conn, "POST", "/companion/target", bodyBytes, pairing);
            conn.setFixedLengthStreamingMode(bodyBytes.length);

            OutputStream out = conn.getOutputStream();
            out.write(bodyBytes);
            out.close();

            int status = conn.getResponseCode();
            updateTargetDeclarationConfirmation(connected, status);
            Log.i(TAG, "target declaration posted url=" + trimmedBaseUrl
                + " status=" + status + " connected=" + connected + " width=" + width + " height=" + height);
        } catch (Exception e) {
            updateTargetDeclarationConfirmation(connected, 0);
            Log.i(TAG, "target declaration failed url=" + baseUrl + ": " + e.getClass().getSimpleName());
        } finally {
            if (conn != null) conn.disconnect();
        }
    }

    private void updateTargetDeclarationConfirmation(boolean connected, int status) {
        long now = System.currentTimeMillis();
        boolean confirmed = connected && status >= 200 && status < 300;
        if (confirmed) {
            targetDeclarationConfirmedUntilMs = now + TARGET_LEASE_MS;
        } else if (!connected || now >= targetDeclarationConfirmedUntilMs) {
            targetDeclarationConfirmedUntilMs = 0;
        }
        handler.post(new Runnable() {
            @Override
            public void run() {
                updateNotification();
            }
        });
    }

    private boolean isTargetDeclarationConfirmed() {
        return targetDeclarationConfirmedUntilMs > System.currentTimeMillis();
    }

    static SharedPreferences getCompanionPreferences(Context context) {
        if (Build.VERSION.SDK_INT < 24) {
            return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        }

        Context deviceContext = context.createDeviceProtectedStorageContext();
        deviceContext.moveSharedPreferencesFrom(context, PREFS);
        return deviceContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    static String getConfiguredJetKvmUrlsText(SharedPreferences prefs) {
        String[] urls = getConfiguredJetKvmUrls(prefs);
        StringBuilder builder = new StringBuilder();
        for (String url : urls) {
            if (builder.length() > 0) builder.append('\n');
            builder.append(url);
        }
        return builder.toString();
    }

    static boolean saveJetKvmUrlsText(SharedPreferences prefs, String rawText) {
        String[] urls = parseJetKvmUrls(rawText);
        StringBuilder builder = new StringBuilder();
        for (String url : urls) {
            if (builder.length() > 0) builder.append('\n');
            builder.append(url);
        }
        return prefs.edit()
            .putString(KEY_JETKVM_URLS, builder.toString())
            .putString(KEY_JETKVM_URL, urls.length == 0 ? DEFAULT_JETKVM_URL : urls[0])
            .commit();
    }

    static boolean addJetKvmUrl(SharedPreferences prefs, String rawUrl) {
        LinkedHashSet<String> urls = new LinkedHashSet<String>();
        String[] existingUrls = getConfiguredJetKvmUrls(prefs);
        for (String existingUrl : existingUrls) {
            urls.add(existingUrl);
        }
        String normalized = normalizeJetKvmUrl(rawUrl);
        if (normalized.length() > 0) {
            urls.add(normalized);
        }
        return saveJetKvmUrlsText(prefs, joinUrls(urls));
    }

    static String[] getConfiguredJetKvmUrls(SharedPreferences prefs) {
        String rawText = prefs.getString(KEY_JETKVM_URLS, null);
        if (rawText == null || rawText.trim().length() == 0) {
            rawText = prefs.getString(KEY_JETKVM_URL, DEFAULT_JETKVM_URL);
        }
        return parseJetKvmUrls(rawText);
    }

    static String[] getPairedJetKvmUrls(SharedPreferences prefs) {
        LinkedHashSet<String> pairedUrls = new LinkedHashSet<String>();
        CompanionPairing[] pairings = getSavedPairings(prefs);
        for (CompanionPairing pairing : pairings) {
            pairedUrls.add(pairing.url);
        }
        return pairedUrls.toArray(new String[pairedUrls.size()]);
    }

    static CompanionPairing[] getSavedPairings(SharedPreferences prefs) {
        LinkedHashSet<String> lines = new LinkedHashSet<String>();
        String rawText = prefs.getString(KEY_JETKVM_PAIRINGS, "");
        String[] rawLines = rawText.split("\\n");
        for (String line : rawLines) {
            CompanionPairing pairing = CompanionPairing.fromLine(line);
            if (pairing != null) {
                lines.add(pairing.toLine());
            }
        }

        CompanionPairing[] pairings = new CompanionPairing[lines.size()];
        int i = 0;
        for (String line : lines) {
            pairings[i++] = CompanionPairing.fromLine(line);
        }
        return pairings;
    }

    static String getPairingCompanionId(SharedPreferences prefs, String rawUrl) {
        CompanionPairing pairing = getPairing(prefs, rawUrl);
        return pairing == null ? "" : pairing.companionId;
    }

    static String getPairingIdentityToken(SharedPreferences prefs, String rawUrl) {
        CompanionPairing pairing = getPairing(prefs, rawUrl);
        return pairing == null ? "" : pairing.identityToken;
    }

    static String[] getPairedJetKvmIdentityTokens(SharedPreferences prefs) {
        LinkedHashSet<String> tokens = new LinkedHashSet<String>();
        String rawText = prefs.getString(KEY_JETKVM_PAIRINGS, "");
        String[] lines = rawText.split("\\n");
        for (String line : lines) {
            CompanionPairing pairing = CompanionPairing.fromLine(line);
            if (pairing != null && pairing.identityToken.length() > 0) {
                tokens.add(pairing.identityToken.toLowerCase(Locale.US));
            }
        }
        return tokens.toArray(new String[tokens.size()]);
    }

    static CompanionPairing getPairing(SharedPreferences prefs, String rawUrl) {
        String normalizedUrl = normalizeJetKvmUrl(rawUrl);
        if (normalizedUrl.length() == 0) return null;

        String rawText = prefs.getString(KEY_JETKVM_PAIRINGS, "");
        String[] lines = rawText.split("\\n");
        for (String line : lines) {
            CompanionPairing pairing = CompanionPairing.fromLine(line);
            if (pairing != null && normalizedUrl.equals(pairing.url)) {
                return pairing;
            }
        }
        return null;
    }

    static boolean savePairing(SharedPreferences prefs, String rawUrl, String companionId, String privateKey, String identityToken) {
        String normalizedUrl = normalizeJetKvmUrl(rawUrl);
        if (normalizedUrl.length() == 0 || companionId == null || companionId.trim().length() == 0 ||
                privateKey == null || privateKey.trim().length() == 0) {
            return false;
        }

        LinkedHashSet<String> lines = new LinkedHashSet<String>();
        String rawText = prefs.getString(KEY_JETKVM_PAIRINGS, "");
        String[] existingLines = rawText.split("\\n");
        for (String line : existingLines) {
            CompanionPairing existing = CompanionPairing.fromLine(line);
            if (existing != null && !normalizedUrl.equals(existing.url)) {
                lines.add(existing.toLine());
            }
        }
        lines.add(new CompanionPairing(
            normalizedUrl,
            companionId.trim(),
            privateKey.trim(),
            identityToken == null ? "" : identityToken.trim().toLowerCase(Locale.US)
        ).toLine());
        return prefs.edit().putString(KEY_JETKVM_PAIRINGS, joinUrls(lines)).commit();
    }

    static boolean removePairing(SharedPreferences prefs, String rawUrl) {
        String normalizedUrl = normalizeJetKvmUrl(rawUrl);
        if (normalizedUrl.length() == 0) return false;

        LinkedHashSet<String> lines = new LinkedHashSet<String>();
        String rawText = prefs.getString(KEY_JETKVM_PAIRINGS, "");
        String[] existingLines = rawText.split("\\n");
        for (String line : existingLines) {
            CompanionPairing existing = CompanionPairing.fromLine(line);
            if (existing != null && !normalizedUrl.equals(existing.url)) {
                lines.add(existing.toLine());
            }
        }
        return prefs.edit().putString(KEY_JETKVM_PAIRINGS, joinUrls(lines)).commit();
    }

    private static String[] parseJetKvmUrls(String rawText) {
        LinkedHashSet<String> normalizedUrls = new LinkedHashSet<String>();
        String text = rawText == null ? "" : rawText;
        String[] parts = text.split("[,\\s]+");
        for (String part : parts) {
            String normalized = normalizeJetKvmUrl(part);
            if (normalized.length() > 0) {
                normalizedUrls.add(normalized);
            }
        }
        if (normalizedUrls.isEmpty()) {
            normalizedUrls.add(DEFAULT_JETKVM_URL);
        }
        return normalizedUrls.toArray(new String[normalizedUrls.size()]);
    }

    private static String normalizeJetKvmUrl(String rawUrl) {
        String url = rawUrl == null ? "" : rawUrl.trim();
        if (url.length() == 0) return "";
        if (!url.contains("://")) {
            url = "http://" + url;
        }
        while (url.endsWith("/") && url.length() > "http://".length()) {
            url = url.substring(0, url.length() - 1);
        }
        return url;
    }

    private static String joinUrls(LinkedHashSet<String> urls) {
        StringBuilder builder = new StringBuilder();
        for (String url : urls) {
            if (builder.length() > 0) builder.append('\n');
            builder.append(url);
        }
        return builder.toString();
    }

    static void applyCompanionSignatureHeaders(HttpURLConnection conn, String method, String path, byte[] bodyBytes, CompanionPairing pairing) throws Exception {
        String timestamp = java.time.Instant.now().toString();
        String nonce = UUID.randomUUID().toString() + "-" + Long.toHexString(new SecureRandom().nextLong());
        String bodyHash = hex(MessageDigest.getInstance("SHA-256").digest(bodyBytes));
        String canonical = method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + bodyHash;

        byte[] privateKeyBytes = android.util.Base64.decode(pairing.privateKey, android.util.Base64.NO_WRAP);
        PrivateKey privateKey = KeyFactory.getInstance("EC").generatePrivate(new PKCS8EncodedKeySpec(privateKeyBytes));
        Signature signer = Signature.getInstance("SHA256withECDSA");
        signer.initSign(privateKey);
        signer.update(canonical.getBytes(StandardCharsets.UTF_8));
        String signature = android.util.Base64.encodeToString(signer.sign(), android.util.Base64.NO_WRAP);

        conn.setRequestProperty("X-JetKVM-Companion-ID", pairing.companionId);
        conn.setRequestProperty("X-JetKVM-Timestamp", timestamp);
        conn.setRequestProperty("X-JetKVM-Nonce", nonce);
        conn.setRequestProperty("X-JetKVM-Signature", signature);
    }

    private static String hex(byte[] bytes) {
        char[] out = new char[bytes.length * 2];
        char[] table = "0123456789abcdef".toCharArray();
        for (int i = 0; i < bytes.length; i++) {
            int value = bytes[i] & 0xff;
            out[i * 2] = table[value >>> 4];
            out[i * 2 + 1] = table[value & 0x0f];
        }
        return new String(out);
    }

    static final class CompanionPairing {
        final String url;
        final String companionId;
        final String privateKey;
        final String identityToken;

        CompanionPairing(String url, String companionId, String privateKey, String identityToken) {
            this.url = url;
            this.companionId = companionId;
            this.privateKey = privateKey;
            this.identityToken = identityToken;
        }

        static CompanionPairing fromLine(String line) {
            if (line == null) return null;
            String[] parts = line.split("\\|", -1);
            if (parts.length < 4) return null;
            String url = normalizeJetKvmUrl(parts[0]);
            String companionId = parts[1].trim();
            String privateKey = parts[2].trim();
            String identityToken = parts[3].trim().toLowerCase(Locale.US);
            if (url.length() == 0 || companionId.length() == 0 || privateKey.length() == 0) return null;
            return new CompanionPairing(url, companionId, privateKey, identityToken);
        }

        String toLine() {
            return url + "|" + companionId + "|" + privateKey + "|" + identityToken;
        }
    }

    private void pulseTargetPresentation(String reason) {
        Display display = findJetKvmPresentationDisplay(reason);
        if (display == null) {
            dismissTargetPresentation("noJetKvmDisplay:" + reason);
            return;
        }

        int displayId = display.getDisplayId();
        dismissTargetPresentation("replace:" + reason);
        try {
            targetPresentation = new TargetPresentation(this, display);
            targetPresentation.show();
            targetPresentationDisplayId = displayId;
            handler.removeCallbacks(dismissTargetPresentationRunnable);
            handler.postDelayed(dismissTargetPresentationRunnable, TARGET_PRESENTATION_PULSE_MS);
            Log.i(TAG, "target presentation pulse shown reason=" + reason
                + " durationMs=" + TARGET_PRESENTATION_PULSE_MS + " " + describeDisplay(display));
        } catch (WindowManager.InvalidDisplayException e) {
            targetPresentation = null;
            targetPresentationDisplayId = -1;
            Log.i(TAG, "target presentation invalid display reason=" + reason);
        } catch (RuntimeException e) {
            targetPresentation = null;
            targetPresentationDisplayId = -1;
            Log.i(TAG, "target presentation failed reason=" + reason + ": " + e.getClass().getSimpleName());
        }
    }

    private Display findJetKvmPresentationDisplay(String reason) {
        if (displayManager == null) return null;

        Display[] presentationDisplays = displayManager.getDisplays(DisplayManager.DISPLAY_CATEGORY_PRESENTATION);
        for (Display display : presentationDisplays) {
            if (isJetKvmExternalDisplay(display)) {
                return display;
            }
        }

        Display[] displays = displayManager.getDisplays();
        for (Display display : displays) {
            if (isJetKvmExternalDisplay(display)) {
                return display;
            }
        }
        logExternalDisplays("no JetKVM display for presentation reason=" + reason);
        return null;
    }

    private boolean isJetKvmExternalDisplay(Display display) {
        return isJetKvmExternalDisplayStatic(
            display,
            getPairedJetKvmIdentityTokens(getCompanionPreferences(this))
        );
    }

    private static boolean isJetKvmExternalDisplayStatic(Display display) {
        return isJetKvmExternalDisplayStatic(display, null);
    }

    private static boolean isJetKvmExternalDisplayStatic(Display display, String[] expectedIdentityTokens) {
        if (display == null || display.getDisplayId() == Display.DEFAULT_DISPLAY) {
            return false;
        }

        String name = display.getName();
        if (name == null) return false;
        String normalizedName = name.toLowerCase(Locale.US);
        if (!normalizedName.contains(JETKVM_DISPLAY_NAME_TOKEN)
                && !normalizedName.contains(JETKVM_SHORT_DISPLAY_NAME_TOKEN)) {
            return false;
        }
        return identityMatches(normalizedName, expectedIdentityTokens);
    }

    private void logExternalDisplays(String reason) {
        if (displayManager == null) return;
        Display[] displays = displayManager.getDisplays();
        StringBuilder builder = new StringBuilder(reason);
        boolean foundExternal = false;
        for (Display display : displays) {
            if (display != null && display.getDisplayId() != Display.DEFAULT_DISPLAY) {
                foundExternal = true;
                builder.append(" candidate=").append(describeDisplay(display));
            }
        }
        if (!foundExternal) {
            builder.append("; no external displays reported");
        }
        Log.i(TAG, builder.toString());
    }

    private String describeDisplay(Display display) {
        if (display == null) return "display=null";
        return "displayId=" + display.getDisplayId() + " name=\"" + display.getName() + "\"";
    }

    private void dismissTargetPresentation(String reason) {
        handler.removeCallbacks(dismissTargetPresentationRunnable);
        if (targetPresentation == null) return;
        try {
            targetPresentation.dismiss();
            Log.i(TAG, "target presentation dismissed reason=" + reason);
        } catch (RuntimeException e) {
            Log.i(TAG, "target presentation dismiss failed reason=" + reason
                + ": " + e.getClass().getSimpleName());
        }
        targetPresentation = null;
        targetPresentationDisplayId = -1;
    }

    private void launchDismissActivity(String action) {
        Intent activity = new Intent(this, DismissActivity.class);
        activity.setAction(action);
        activity.addFlags(
            Intent.FLAG_ACTIVITY_NEW_TASK
                | Intent.FLAG_ACTIVITY_SINGLE_TOP
                | Intent.FLAG_ACTIVITY_NO_HISTORY
                | Intent.FLAG_ACTIVITY_EXCLUDE_FROM_RECENTS
        );
        Log.i(TAG, "starting dismiss activity action=" + action);
        try {
            startActivity(activity);
        } catch (RuntimeException e) {
            Log.i(TAG, "starting dismiss activity failed: " + e.getClass().getSimpleName());
        }
    }

    private void ensureLaunchAssistOverlay() {
        if (!Settings.canDrawOverlays(this)) {
            Log.i(TAG, "overlay permission not granted; background dismiss launch may be blocked");
            return;
        }
        if (launchAssistOverlay != null) return;

        windowManager = (WindowManager) getSystemService(Context.WINDOW_SERVICE);
        launchAssistOverlay = new View(this);
        launchAssistOverlay.setAlpha(0.01f);

        WindowManager.LayoutParams params = new WindowManager.LayoutParams(
            1,
            1,
            WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,
            WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
                | WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE
                | WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL
                | WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
            PixelFormat.TRANSLUCENT
        );
        params.gravity = Gravity.TOP | Gravity.START;
        params.x = 0;
        params.y = 0;

        try {
            windowManager.addView(launchAssistOverlay, params);
            Log.i(TAG, "launch assist overlay added");
        } catch (RuntimeException e) {
            Log.i(TAG, "launch assist overlay failed: " + e.getClass().getSimpleName());
            launchAssistOverlay = null;
        }
    }

    private void removeLaunchAssistOverlay() {
        if (windowManager == null || launchAssistOverlay == null) return;
        try {
            windowManager.removeView(launchAssistOverlay);
        } catch (RuntimeException ignored) {
        }
        launchAssistOverlay = null;
    }

    private void createChannel() {
        if (android.os.Build.VERSION.SDK_INT < 26) return;
        NotificationChannel channel = new NotificationChannel(
            CHANNEL_ID,
            "JetKVM Companion",
            NotificationManager.IMPORTANCE_LOW
        );
        NotificationManager manager = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        manager.createNotificationChannel(channel);
    }

    private Notification buildNotification() {
        Intent intent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(
            this,
            0,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE
        );
        Notification.Builder builder = android.os.Build.VERSION.SDK_INT >= 26
            ? new Notification.Builder(this, CHANNEL_ID)
            : new Notification.Builder(this);
        String body;
        if (!hasPairedJetKvmEndpoints) {
            body = "Waiting for a device to be paired...";
        } else if (!jetkvmPeripheralsPresent) {
            body = "Waiting for peripherals...";
        } else if (!isTargetDeclarationConfirmed()) {
            body = "Waiting for backend confirmation...";
        } else {
            body = "Monitoring display-on events";
        }
        builder
            .setSmallIcon(getApplicationInfo().icon)
            .setContentTitle("JetKVM Companion")
            .setContentText(body)
            .setContentIntent(pendingIntent)
            .setOngoing(true);
        if (!hasPairedJetKvmEndpoints) {
            builder.addAction(getApplicationInfo().icon, "Pair", pendingIntent);
        }
        return builder.build();
    }

    private void updateNotification() {
        NotificationManager manager = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (manager != null) {
            manager.notify(NOTIFICATION_ID, buildNotification());
        }
    }

    private void startPairingRequestServer() {
        if (pairingServerThread != null) return;
        pairingServerThread = new Thread(new Runnable() {
            @Override
            public void run() {
                try {
                    pairingServerSocket = new ServerSocket(PAIRING_LISTEN_PORT);
                    while (!Thread.currentThread().isInterrupted()) {
                        handlePairingRequestSocket(pairingServerSocket.accept());
                    }
                } catch (Exception e) {
                    Log.w(TAG, "pairing request listener stopped: " + e.getClass().getSimpleName());
                }
            }
        }, "JetKVM-pair-listener");
        pairingServerThread.start();
    }

    private void stopPairingRequestServer() {
        if (pairingServerThread != null) {
            pairingServerThread.interrupt();
            pairingServerThread = null;
        }
        if (pairingServerSocket != null) {
            try {
                pairingServerSocket.close();
            } catch (Exception ignored) {
            }
            pairingServerSocket = null;
        }
    }

    private void handlePairingRequestSocket(Socket socket) {
        try {
            BufferedReader reader = new BufferedReader(new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8));
            String requestLine = reader.readLine();
            int contentLength = 0;
            String line;
            while ((line = reader.readLine()) != null && line.length() > 0) {
                String lower = line.toLowerCase(Locale.US);
                if (lower.startsWith("content-length:")) {
                    contentLength = Integer.parseInt(line.substring(line.indexOf(':') + 1).trim());
                }
            }
            char[] chars = new char[Math.max(0, contentLength)];
            int read = 0;
            while (read < chars.length) {
                int count = reader.read(chars, read, chars.length - read);
                if (count < 0) break;
                read += count;
            }

            String body = new String(chars, 0, read);
            String jetkvmUrl = extractJsonString(body, "jetkvm_url");
            String requestId = extractJsonString(body, "request_id");
            boolean ok = requestLine != null
                && requestLine.startsWith("POST /pair/request ")
                && jetkvmUrl.length() > 0
                && requestId.length() > 0;
            if (ok) {
                showPairingRequestNotification(jetkvmUrl, requestId);
                writePairingServerResponse(socket, 202, "{\"status\":\"pending\"}");
            } else {
                writePairingServerResponse(socket, 400, "{\"error\":\"invalid pairing request\"}");
            }
        } catch (Exception e) {
            Log.w(TAG, "pairing request failed: " + e.getClass().getSimpleName());
        } finally {
            try {
                socket.close();
            } catch (Exception ignored) {
            }
        }
    }

    private void writePairingServerResponse(Socket socket, int status, String body) throws java.io.IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        String reason = status == 202 ? "Accepted" : "Bad Request";
        OutputStream out = socket.getOutputStream();
        out.write(("HTTP/1.1 " + status + " " + reason + "\r\nContent-Type: application/json\r\nContent-Length: " + bytes.length + "\r\nConnection: close\r\n\r\n").getBytes(StandardCharsets.UTF_8));
        out.write(bytes);
        out.flush();
    }

    private void showPairingRequestNotification(String jetkvmUrl, String requestId) {
        Intent intent = new Intent(this, MainActivity.class);
        intent.putExtra(EXTRA_JETKVM_URL, jetkvmUrl);
        intent.putExtra(EXTRA_PAIR_REQUEST_ID, requestId);
        PendingIntent pendingIntent = PendingIntent.getActivity(
            this,
            PAIRING_NOTIFICATION_ID,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE
        );
        Notification.Builder builder = android.os.Build.VERSION.SDK_INT >= 26
            ? new Notification.Builder(this, CHANNEL_ID)
            : new Notification.Builder(this);
        Notification notification = builder
            .setSmallIcon(getApplicationInfo().icon)
            .setContentTitle("JetKVM pairing request")
            .setContentText("Open companion to pair with " + jetkvmUrl)
            .setContentIntent(pendingIntent)
            .setAutoCancel(true)
            .build();
        NotificationManager manager = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (manager != null) {
            manager.notify(PAIRING_NOTIFICATION_ID, notification);
        }
    }

    private static String extractJsonString(String json, String key) {
        if (json == null || key == null) return "";
        String needle = "\"" + key + "\"";
        int keyIndex = json.indexOf(needle);
        if (keyIndex < 0) return "";
        int colonIndex = json.indexOf(':', keyIndex + needle.length());
        if (colonIndex < 0) return "";
        int startQuote = json.indexOf('"', colonIndex + 1);
        if (startQuote < 0) return "";
        int endQuote = json.indexOf('"', startQuote + 1);
        if (endQuote < 0) return "";
        return json.substring(startQuote + 1, endQuote);
    }

    private static final class TargetPresentation extends Presentation {
        TargetPresentation(Context context, Display display) {
            super(context, display, R.style.TransparentPresentation);
        }

        @Override
        protected void onCreate(Bundle savedInstanceState) {
            super.onCreate(savedInstanceState);
            requestWindowFeature(Window.FEATURE_NO_TITLE);

            View anchor = new View(getContext());
            anchor.setAlpha(0.01f);
            anchor.setKeepScreenOn(true);
            FrameLayout root = new FrameLayout(getContext());
            root.setBackgroundColor(Color.TRANSPARENT);
            root.setKeepScreenOn(true);
            root.addView(anchor, new FrameLayout.LayoutParams(1, 1));
            setContentView(root);

            Window window = getWindow();
            if (window != null) {
                window.setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
                window.clearFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND);
                window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
                WindowManager.LayoutParams attrs = window.getAttributes();
                attrs.dimAmount = 0f;
                attrs.gravity = Gravity.TOP | Gravity.START;
                attrs.x = 0;
                attrs.y = 0;
                window.setAttributes(attrs);
            }
        }

        @Override
        protected void onStart() {
            super.onStart();
            Window window = getWindow();
            if (window != null) {
                window.setLayout(1, 1);
            }
        }
    }

    private static final class JetKvmInputIdentity {
        final boolean isJetKvm;
        final boolean usesLinuxGadgetIds;

        private JetKvmInputIdentity(boolean isJetKvm, boolean usesLinuxGadgetIds) {
            this.isJetKvm = isJetKvm;
            this.usesLinuxGadgetIds = usesLinuxGadgetIds;
        }

        static JetKvmInputIdentity from(InputDevice device, String[] expectedIdentityTokens) {
            String name = device.getName();
            String normalizedName = name == null ? "" : name.toLowerCase(java.util.Locale.US);
            boolean nameMatches = normalizedName.contains(JETKVM_INPUT_NAME_TOKEN)
                && identityMatches(normalizedName, expectedIdentityTokens);

            int vendorId = 0;
            int productId = 0;
            if (Build.VERSION.SDK_INT >= 19) {
                vendorId = device.getVendorId();
                productId = device.getProductId();
            }

            boolean linuxGadgetIds = vendorId == LINUX_GADGET_VENDOR_ID
                && productId == LINUX_GADGET_PRODUCT_ID;

            if (!nameMatches) {
                return new JetKvmInputIdentity(false, linuxGadgetIds);
            }

            if (vendorId != 0 && productId != 0 && !linuxGadgetIds) {
                Log.i(TAG, "JetKVM-named input uses non-default vid/pid vendor="
                    + vendorId + " product=" + productId);
            }

            return new JetKvmInputIdentity(true, linuxGadgetIds);
        }
    }

    private static boolean identityMatches(String text, String[] expectedIdentityTokens) {
        if (expectedIdentityTokens == null || expectedIdentityTokens.length == 0) {
            return true;
        }
        if (text == null) return false;
        String normalizedText = text.toLowerCase(Locale.US);
        for (String token : expectedIdentityTokens) {
            if (token != null && token.trim().length() > 0
                    && normalizedText.contains(token.trim().toLowerCase(Locale.US))) {
                return true;
            }
        }
        return false;
    }

    static final class JetKvmPeripheralSnapshot {
        boolean keyboard;
        boolean touchscreen;
        boolean pointer;
        boolean monitor;
        boolean present;
        int deviceCount;
        int linuxGadgetIdCount;
        int displayCount;

        String toJsonEvidence() {
            StringBuilder builder = new StringBuilder();
            appendEvidence(builder, keyboard, "keyboard");
            appendEvidence(builder, touchscreen, "digitizer");
            appendEvidence(builder, pointer, "mouse");
            appendEvidence(builder, monitor, "monitor");
            return builder.toString();
        }

        private static void appendEvidence(StringBuilder builder, boolean enabled, String value) {
            if (!enabled) return;
            if (builder.length() > 0) builder.append(',');
            builder.append('"').append(value).append('"');
        }

        @Override
        public String toString() {
            return "devices=" + deviceCount
                + " linuxGadgetIds=" + linuxGadgetIdCount
                + " displays=" + displayCount
                + " keyboard=" + keyboard
                + " touchscreen=" + touchscreen
                + " pointer=" + pointer
                + " monitor=" + monitor
                + " present=" + present;
        }
    }
}
