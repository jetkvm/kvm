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
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashSet;
import java.util.Locale;

public class CompanionService extends Service implements InputManager.InputDeviceListener {
    static final String TAG = "JetKVMCompanion";
    static final String ACTION_SCREEN_ON = "com.jetkvm.companion.SCREEN_ON";
    static final String PREFS = "jetkvm_companion";
    static final String KEY_LAUNCH_ON_BOOT = "launch_on_boot";
    static final String KEY_JETKVM_URL = "jetkvm_url";
    static final String KEY_JETKVM_URLS = "jetkvm_urls";
    static final String DEFAULT_JETKVM_URL = "http://jetkvm.local";
    static final String EXTRA_JETKVM_URL = "jetkvm_url";

    private static final String CHANNEL_ID = "jetkvm-companion";
    private static final int NOTIFICATION_ID = 1001;
    private static final long SCREEN_ON_DISMISS_DELAY_MS = 600;
    private static final long TARGET_REPORT_INTERVAL_MS = 15000;
    private static final long TARGET_PRESENTATION_PULSE_MS = 750;
    private static final String JETKVM_INPUT_NAME_TOKEN = "jetkvm";
    private static final String JETKVM_DISPLAY_NAME_TOKEN = "jetkvm";
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
    private boolean jetkvmPeripheralsPresent;
    private boolean attemptedForCurrentScreen;
    private boolean targetReportScheduled;

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
                pulseTargetPresentation("screen_on");
                if (attemptedForCurrentScreen) {
                    Log.i(TAG, "screen-on ignored; dismiss already attempted for this screen cycle");
                    return;
                }
                attemptedForCurrentScreen = true;
                handler.postDelayed(new Runnable() {
                    @Override
                    public void run() {
                        if (jetkvmPeripheralsPresent) {
                            launchDismissActivity(ACTION_SCREEN_ON);
                        } else {
                            Log.i(TAG, "pending dismiss cancelled; JetKVM peripherals removed");
                        }
                    }
                }, SCREEN_ON_DISMISS_DELAY_MS);
            }
        }
    };

    private final DisplayManager.DisplayListener displayListener = new DisplayManager.DisplayListener() {
        @Override
        public void onDisplayAdded(int displayId) {
            if (jetkvmPeripheralsPresent && isJetKvmExternalDisplay(displayManager.getDisplay(displayId))) {
                pulseTargetPresentation("displayAdded:" + displayId);
            } else {
                Log.i(TAG, "display added ignored " + describeDisplay(displayManager.getDisplay(displayId)));
            }
        }

        @Override
        public void onDisplayChanged(int displayId) {
            if (jetkvmPeripheralsPresent && isJetKvmExternalDisplay(displayManager.getDisplay(displayId))) {
                Log.i(TAG, "display changed observed " + describeDisplay(displayManager.getDisplay(displayId)));
            } else {
                Log.i(TAG, "display changed ignored " + describeDisplay(displayManager.getDisplay(displayId)));
            }
        }

        @Override
        public void onDisplayRemoved(int displayId) {
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
        JetKvmPeripheralSnapshot snapshot = new JetKvmPeripheralSnapshot();
        int[] ids = InputDevice.getDeviceIds();
        for (int id : ids) {
            InputDevice device = InputDevice.getDevice(id);
            if (device == null) {
                continue;
            }

            JetKvmInputIdentity identity = JetKvmInputIdentity.from(device);
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
        snapshot.present = snapshot.keyboard && (snapshot.touchscreen || snapshot.pointer);
        return snapshot;
    }

    private void updateJetKvmPeripheralState(String reason) {
        JetKvmPeripheralSnapshot snapshot = getJetKvmPeripheralSnapshot();
        if (snapshot.present != jetkvmPeripheralsPresent) {
            jetkvmPeripheralsPresent = snapshot.present;
            attemptedForCurrentScreen = false;
            if (!jetkvmPeripheralsPresent) {
                targetReportScheduled = false;
                handler.removeCallbacksAndMessages(null);
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
        final String[] jetkvmUrls = getConfiguredJetKvmUrls(getCompanionPreferences(this));
        final DisplayMetrics metrics = getResources().getDisplayMetrics();
        final int width = Math.min(metrics.widthPixels, metrics.heightPixels);
        final int height = Math.max(metrics.widthPixels, metrics.heightPixels);
        if (width <= 0 || height <= 0) return;

        new Thread(new Runnable() {
            @Override
            public void run() {
                for (String jetkvmUrl : jetkvmUrls) {
                    postTargetDeclaration(jetkvmUrl, width, height);
                }
            }
        }, "JetKVM-target-report").start();
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

    private void postTargetDeclaration(String baseUrl, int width, int height) {
        HttpURLConnection conn = null;
        try {
            String trimmedBaseUrl = baseUrl == null ? "" : baseUrl.trim();
            if (trimmedBaseUrl.length() == 0) trimmedBaseUrl = DEFAULT_JETKVM_URL;
            while (trimmedBaseUrl.endsWith("/")) {
                trimmedBaseUrl = trimmedBaseUrl.substring(0, trimmedBaseUrl.length() - 1);
            }

            URL url = new URL(trimmedBaseUrl + "/companion/target");
            String body = String.format(
                Locale.US,
                "{\"target_type\":\"android\",\"target_mode\":\"android_mirror\",\"display_width\":%d,\"display_height\":%d,\"display_aspect\":%.8f}",
                width,
                height,
                (double) width / (double) height
            );
            byte[] bodyBytes = body.getBytes(StandardCharsets.UTF_8);

            conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(3000);
            conn.setReadTimeout(3000);
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.setRequestProperty("Content-Type", "application/json; charset=utf-8");
            conn.setFixedLengthStreamingMode(bodyBytes.length);

            OutputStream out = conn.getOutputStream();
            out.write(bodyBytes);
            out.close();

            int status = conn.getResponseCode();
            Log.i(TAG, "target declaration posted url=" + trimmedBaseUrl
                + " status=" + status + " width=" + width + " height=" + height);
        } catch (Exception e) {
            Log.i(TAG, "target declaration failed url=" + baseUrl + ": " + e.getClass().getSimpleName());
        } finally {
            if (conn != null) conn.disconnect();
        }
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
        if (display == null || display.getDisplayId() == Display.DEFAULT_DISPLAY) {
            return false;
        }

        String name = display.getName();
        if (name == null) return false;
        return name.toLowerCase(Locale.US).contains(JETKVM_DISPLAY_NAME_TOKEN);
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
        return builder
            .setSmallIcon(getApplicationInfo().icon)
            .setContentTitle("JetKVM Companion")
            .setContentText(jetkvmPeripheralsPresent
                ? "JetKVM target peripherals detected"
                : "Waiting for JetKVM target peripherals")
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build();
    }

    private void updateNotification() {
        NotificationManager manager = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        if (manager != null) {
            manager.notify(NOTIFICATION_ID, buildNotification());
        }
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

        static JetKvmInputIdentity from(InputDevice device) {
            String name = device.getName();
            boolean nameMatches = name != null
                && name.toLowerCase(java.util.Locale.US).contains(JETKVM_INPUT_NAME_TOKEN);

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

    static final class JetKvmPeripheralSnapshot {
        boolean keyboard;
        boolean touchscreen;
        boolean pointer;
        boolean present;
        int deviceCount;
        int linuxGadgetIdCount;

        @Override
        public String toString() {
            return "devices=" + deviceCount
                + " linuxGadgetIds=" + linuxGadgetIdCount
                + " keyboard=" + keyboard
                + " touchscreen=" + touchscreen
                + " pointer=" + pointer
                + " present=" + present;
        }
    }
}
