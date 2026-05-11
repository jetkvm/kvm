package com.jetkvm.companion;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.content.res.ColorStateList;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.PowerManager;
import android.provider.Settings;
import android.text.InputType;
import android.view.Gravity;
import android.view.Window;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.CompoundButton;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

public class MainActivity extends Activity {
    private static final int REQUEST_POST_NOTIFICATIONS = 10;
    private static final int JETKVM_BACKGROUND = Color.rgb(7, 12, 28);
    private static final int JETKVM_BLUE_700 = Color.rgb(20, 71, 230);
    private static final String EXTRA_JETKVM_URL = "jetkvm_url";

    private SharedPreferences prefs;
    private CheckBox launchOnBootInput;
    private EditText jetkvmUrlsInput;
    private Button notificationButton;
    private Button overlayButton;
    private Button batteryButton;
    private TextView statusText;
    private boolean requestedNotificationThisLaunch;
    private boolean requestedOverlayThisLaunch;
    private boolean requestedBatteryThisLaunch;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        requestWindowFeature(Window.FEATURE_NO_TITLE);

        prefs = getCompanionPreferences();
        saveJetKvmUrlFromIntent(getIntent());
        startCompanionServiceFromIntent(getIntent());
        setContentView(createSettingsView());
        updateArmStatus();
        requestMissingPermissionsIfNeeded();
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        saveJetKvmUrlFromIntent(intent);
        if (jetkvmUrlsInput != null) {
            jetkvmUrlsInput.setText(CompanionService.getConfiguredJetKvmUrlsText(prefs));
        }
        startCompanionServiceFromIntent(intent);
    }

    @Override
    protected void onResume() {
        super.onResume();
        updateArmStatus();
        requestMissingPermissionsIfNeeded();
    }

    private LinearLayout createSettingsView() {
        int padding = dp(24);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setGravity(Gravity.CENTER_HORIZONTAL);
        root.setPadding(padding, padding, padding, padding);
        root.setBackgroundColor(JETKVM_BACKGROUND);

        TextView title = new TextView(this);
        title.setText("JetKVM Companion");
        title.setTextColor(Color.WHITE);
        title.setTextSize(26);
        title.setGravity(Gravity.CENTER);
        root.addView(title, matchWrap());

        TextView description = new TextView(this);
        description.setText("Target-side helper for JetKVM Android metadata, keyguard, and display handling.");
        description.setTextColor(Color.rgb(203, 213, 225));
        description.setTextSize(15);
        description.setGravity(Gravity.CENTER);
        description.setPadding(0, dp(8), 0, dp(18));
        root.addView(description, matchWrap());

        launchOnBootInput = new CheckBox(this);
        launchOnBootInput.setText("Launch on boot");
        launchOnBootInput.setTextColor(Color.WHITE);
        launchOnBootInput.setTextSize(16);
        launchOnBootInput.setChecked(prefs.getBoolean(CompanionService.KEY_LAUNCH_ON_BOOT, false));
        launchOnBootInput.setOnCheckedChangeListener(new CompoundButton.OnCheckedChangeListener() {
            @Override
            public void onCheckedChanged(CompoundButton buttonView, boolean isChecked) {
                prefs.edit().putBoolean(CompanionService.KEY_LAUNCH_ON_BOOT, isChecked).apply();
                updateStatus(isChecked ? "Launch on boot enabled." : "Launch on boot disabled.");
            }
        });
        root.addView(launchOnBootInput, matchWrap());

        TextView jetkvmUrlsLabel = new TextView(this);
        jetkvmUrlsLabel.setText("JetKVM endpoints");
        jetkvmUrlsLabel.setTextColor(Color.WHITE);
        jetkvmUrlsLabel.setTextSize(16);
        root.addView(jetkvmUrlsLabel, tightWrap());

        jetkvmUrlsInput = new EditText(this);
        jetkvmUrlsInput.setSingleLine(false);
        jetkvmUrlsInput.setMinLines(3);
        jetkvmUrlsInput.setGravity(Gravity.TOP | Gravity.START);
        jetkvmUrlsInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE);
        jetkvmUrlsInput.setText(CompanionService.getConfiguredJetKvmUrlsText(prefs));
        jetkvmUrlsInput.setHint("192.168.8.229\nhttp://jetkvm.local");
        jetkvmUrlsInput.setTextColor(Color.WHITE);
        jetkvmUrlsInput.setHintTextColor(Color.rgb(148, 163, 184));
        root.addView(jetkvmUrlsInput, matchWrap());

        Button saveJetkvmUrlButton = new Button(this);
        saveJetkvmUrlButton.setText("Save JetKVM endpoints");
        saveJetkvmUrlButton.setAllCaps(false);
        applyButtonStyle(saveJetkvmUrlButton);
        saveJetkvmUrlButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                String value = jetkvmUrlsInput.getText().toString();
                CompanionService.saveJetKvmUrlsText(prefs, value);
                jetkvmUrlsInput.setText(CompanionService.getConfiguredJetKvmUrlsText(prefs));
                startForegroundService(new Intent(MainActivity.this, CompanionService.class));
                updateStatus("JetKVM endpoints saved.");
            }
        });
        root.addView(saveJetkvmUrlButton, matchWrap());

        notificationButton = new Button(this);
        notificationButton.setText("Grant permission to post notifications");
        notificationButton.setAllCaps(false);
        applyButtonStyle(notificationButton);
        notificationButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                requestNotificationPermission();
            }
        });
        root.addView(notificationButton, matchWrap());

        overlayButton = new Button(this);
        overlayButton.setText("Grant permission to display over other apps");
        overlayButton.setAllCaps(false);
        applyButtonStyle(overlayButton);
        overlayButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                requestOverlayPermission();
            }
        });
        root.addView(overlayButton, matchWrap());

        batteryButton = new Button(this);
        batteryButton.setText("Grant unrestricted battery usage");
        batteryButton.setAllCaps(false);
        applyButtonStyle(batteryButton);
        batteryButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                requestBatteryOptimizationExemption();
            }
        });
        root.addView(batteryButton, matchWrap());

        statusText = new TextView(this);
        statusText.setTextColor(Color.rgb(148, 163, 184));
        statusText.setTextSize(14);
        statusText.setGravity(Gravity.CENTER);
        statusText.setPadding(0, dp(16), 0, 0);
        root.addView(statusText, matchWrap());

        return root;
    }

    private LinearLayout.LayoutParams matchWrap() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        );
        params.setMargins(0, 0, 0, dp(12));
        return params;
    }

    private LinearLayout.LayoutParams tightWrap() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        );
        params.setMargins(0, 0, 0, dp(4));
        return params;
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void applyButtonStyle(Button button) {
        button.setTextColor(Color.WHITE);
        button.setBackgroundTintList(ColorStateList.valueOf(JETKVM_BLUE_700));
    }

    private void updateStatus(String message) {
        if (statusText != null) statusText.setText(message);
    }

    private void saveJetKvmUrlFromIntent(Intent intent) {
        if (intent == null || !intent.hasExtra(EXTRA_JETKVM_URL)) return;

        String value = intent.getStringExtra(EXTRA_JETKVM_URL);
        if (value == null) return;

        value = value.trim();
        if (value.length() == 0) value = CompanionService.DEFAULT_JETKVM_URL;
        CompanionService.addJetKvmUrl(prefs, value);
    }

    private void startCompanionServiceFromIntent(Intent source) {
        Intent service = new Intent(this, CompanionService.class);
        if (source != null && source.hasExtra(EXTRA_JETKVM_URL)) {
            service.putExtra(
                CompanionService.EXTRA_JETKVM_URL,
                source.getStringExtra(EXTRA_JETKVM_URL)
            );
        }
        startForegroundService(service);
    }

    private void updateArmStatus() {
        CompanionService.JetKvmPeripheralSnapshot snapshot = CompanionService.getJetKvmPeripheralSnapshot();
        boolean notificationGranted = hasNotificationPermission();
        boolean overlayGranted = Settings.canDrawOverlays(this);
        boolean batteryGranted = isIgnoringBatteryOptimizations();

        if (notificationButton != null) {
            notificationButton.setText("Grant permission to post notifications");
        }
        if (overlayButton != null) {
            overlayButton.setText("Grant permission to display over other apps");
        }
        if (batteryButton != null) {
            batteryButton.setText("Grant unrestricted battery usage");
        }

        String notificationStatus = notificationGranted
            ? "Notifications granted"
            : "Notifications not granted";
        String overlayStatus = overlayGranted
            ? "Display over other apps granted"
            : "Display over other apps not granted";
        String batteryStatus = batteryGranted
            ? "Unrestricted battery granted"
            : "Unrestricted battery not granted";
        updateStatus(notificationStatus + "\n" + overlayStatus + "\n" + batteryStatus
            + "\nJetKVM peripherals: " + snapshot);
    }

    private void requestMissingPermissionsIfNeeded() {
        if (!requestedNotificationThisLaunch && !hasNotificationPermission()) {
            requestedNotificationThisLaunch = true;
            requestNotificationPermission();
            return;
        }
        if (!requestedOverlayThisLaunch && !Settings.canDrawOverlays(this)) {
            requestedOverlayThisLaunch = true;
            requestOverlayPermission();
            return;
        }
        if (!requestedBatteryThisLaunch && !isIgnoringBatteryOptimizations()) {
            requestedBatteryThisLaunch = true;
            requestBatteryOptimizationExemption();
        }
    }

    private boolean hasNotificationPermission() {
        return Build.VERSION.SDK_INT < 33
            || checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED;
    }

    private void requestNotificationPermission() {
        if (Build.VERSION.SDK_INT < 33) {
            updateArmStatus();
            return;
        }
        if (hasNotificationPermission()) {
            updateArmStatus();
            return;
        }
        requestPermissions(new String[] { android.Manifest.permission.POST_NOTIFICATIONS }, REQUEST_POST_NOTIFICATIONS);
    }

    private void requestOverlayPermission() {
        Intent intent = new Intent(
            Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
            Uri.parse("package:" + getPackageName())
        );
        startActivity(intent);
    }

    private void requestBatteryOptimizationExemption() {
        Intent intent = new Intent(
            Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
            Uri.parse("package:" + getPackageName())
        );
        try {
            startActivity(intent);
        } catch (ActivityNotFoundException e) {
            openAppSettings();
        }
    }

    private void openAppSettings() {
        Intent intent = new Intent(
            Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
            Uri.parse("package:" + getPackageName())
        );
        startActivity(intent);
    }

    private SharedPreferences getCompanionPreferences() {
        return CompanionService.getCompanionPreferences(this);
    }

    private boolean isIgnoringBatteryOptimizations() {
        PowerManager powerManager = (PowerManager) getSystemService(POWER_SERVICE);
        return powerManager == null || powerManager.isIgnoringBatteryOptimizations(getPackageName());
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == REQUEST_POST_NOTIFICATIONS) {
            updateArmStatus();
            requestMissingPermissionsIfNeeded();
        }
    }
}
