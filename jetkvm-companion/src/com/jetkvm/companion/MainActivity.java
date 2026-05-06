package com.jetkvm.companion;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.PowerManager;
import android.provider.Settings;
import android.view.Gravity;
import android.view.Window;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.CompoundButton;
import android.widget.LinearLayout;
import android.widget.TextView;

public class MainActivity extends Activity {
    private static final int REQUEST_POST_NOTIFICATIONS = 10;

    private SharedPreferences prefs;
    private CheckBox launchOnBootInput;
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
        startForegroundService(new Intent(this, CompanionService.class));
        setContentView(createSettingsView());
        updateArmStatus();
        requestMissingPermissionsIfNeeded();
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
        root.setBackgroundColor(Color.rgb(7, 12, 28));

        TextView title = new TextView(this);
        title.setText("JetKVM Companion");
        title.setTextColor(Color.WHITE);
        title.setTextSize(26);
        title.setGravity(Gravity.CENTER);
        root.addView(title, matchWrap());

        TextView description = new TextView(this);
        description.setText("Target-side helper for trusted Android keyguard dismissal.");
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

        Button armButton = new Button(this);
        armButton.setText("Arm companion");
        armButton.setAllCaps(false);
        armButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                startForegroundService(new Intent(MainActivity.this, CompanionService.class));
                updateArmStatus();
                requestMissingPermissionsIfNeeded();
            }
        });
        root.addView(armButton, matchWrap());

        notificationButton = new Button(this);
        notificationButton.setText("Grant permission to post notifications");
        notificationButton.setAllCaps(false);
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
        batteryButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                requestBatteryOptimizationExemption();
            }
        });
        root.addView(batteryButton, matchWrap());

        Button dismissButton = new Button(this);
        dismissButton.setText("Test trusted keyguard dismiss");
        dismissButton.setAllCaps(false);
        dismissButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                Intent intent = new Intent(MainActivity.this, DismissActivity.class);
                intent.setAction(DismissActivity.ACTION_MANUAL);
                intent.addFlags(Intent.FLAG_ACTIVITY_NO_HISTORY | Intent.FLAG_ACTIVITY_EXCLUDE_FROM_RECENTS);
                startActivity(intent);
            }
        });
        root.addView(dismissButton, matchWrap());

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

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void updateStatus(String message) {
        if (statusText != null) statusText.setText(message);
    }

    private void updateArmStatus() {
        CompanionService.JetKvmPeripheralSnapshot snapshot = CompanionService.getJetKvmPeripheralSnapshot();
        boolean notificationGranted = hasNotificationPermission();
        boolean overlayGranted = Settings.canDrawOverlays(this);
        boolean batteryGranted = isIgnoringBatteryOptimizations();

        if (notificationButton != null) {
            notificationButton.setText("Grant permission to post notifications - "
                + (notificationGranted ? "Granted" : "Needed"));
        }
        if (overlayButton != null) {
            overlayButton.setText("Grant permission to display over other apps - "
                + (overlayGranted ? "Granted" : "Needed"));
        }
        if (batteryButton != null) {
            batteryButton.setText("Grant unrestricted battery usage - "
                + (batteryGranted ? "Granted" : "Needed"));
        }

        String notificationStatus = notificationGranted
            ? "Notifications granted."
            : "Grant notifications so Android can show the required foreground service.";
        String overlayStatus = overlayGranted
            ? "Background launch assist granted."
            : "Grant background launch assist for automatic wake unlock.";
        String batteryStatus = batteryGranted
            ? "Battery unrestricted."
            : "Grant unrestricted battery for boot/watchdog reliability.";
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
        if (Build.VERSION.SDK_INT < 24) {
            return getSharedPreferences(CompanionService.PREFS, MODE_PRIVATE);
        }

        Context credentialContext = this;
        Context deviceContext = createDeviceProtectedStorageContext();
        deviceContext.moveSharedPreferencesFrom(credentialContext, CompanionService.PREFS);
        return deviceContext.getSharedPreferences(CompanionService.PREFS, MODE_PRIVATE);
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
