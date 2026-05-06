package com.jetkvm.companion;

import android.app.Activity;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
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
    private TextView statusText;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        requestWindowFeature(Window.FEATURE_NO_TITLE);

        prefs = getSharedPreferences(CompanionService.PREFS, MODE_PRIVATE);
        startForegroundService(new Intent(this, CompanionService.class));
        setContentView(createSettingsView());
        updateArmStatus();
        requestNotificationPermissionIfNeeded();
    }

    @Override
    protected void onResume() {
        super.onResume();
        updateArmStatus();
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
                requestNotificationPermissionIfNeeded();
            }
        });
        root.addView(armButton, matchWrap());

        Button overlayButton = new Button(this);
        overlayButton.setText("Grant background launch assist");
        overlayButton.setAllCaps(false);
        overlayButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                Intent intent = new Intent(
                    Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                    Uri.parse("package:" + getPackageName())
                );
                startActivity(intent);
            }
        });
        root.addView(overlayButton, matchWrap());

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
        updateStatus(Settings.canDrawOverlays(this)
            ? "Companion service armed with background launch assist."
            : "Companion service armed. Grant background launch assist for automatic wake unlock.");
    }

    private void requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT < 33) return;
        if (checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) return;
        requestPermissions(new String[] { android.Manifest.permission.POST_NOTIFICATIONS }, REQUEST_POST_NOTIFICATIONS);
    }
}
