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
import android.text.Editable;
import android.text.InputType;
import android.text.TextWatcher;
import android.view.Gravity;
import android.view.Window;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.CompoundButton;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import java.io.BufferedReader;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;

public class MainActivity extends Activity {
    private static final int REQUEST_POST_NOTIFICATIONS = 10;
    private static final int JETKVM_BACKGROUND = Color.rgb(7, 12, 28);
    private static final int JETKVM_BLUE_700 = Color.rgb(20, 71, 230);
    private static final String EXTRA_JETKVM_URL = "jetkvm_url";
    private static final String EXTRA_PAIR_REQUEST_ID = "pair_request_id";
    private static final SecureRandom PAIRING_RANDOM = new SecureRandom();

    private SharedPreferences prefs;
    private CheckBox launchOnBootInput;
    private EditText jetkvmUrlsInput;
    private Button pairJetkvmButton;
    private TextView pairJetkvmState;
    private LinearLayout pairingsList;
    private Button notificationButton;
    private Button overlayButton;
    private Button batteryButton;
    private TextView statusText;
    private String pendingPairUrl = "";
    private String pendingPairRequestId = "";
    private boolean requestedNotificationThisLaunch;
    private boolean requestedOverlayThisLaunch;
    private boolean requestedBatteryThisLaunch;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        requestWindowFeature(Window.FEATURE_NO_TITLE);

        prefs = getCompanionPreferences();
        saveJetKvmUrlFromIntent(getIntent());
        savePairRequestFromIntent(getIntent());
        startCompanionServiceFromIntent(getIntent());
        setContentView(createSettingsView());
        updateArmStatus();
        requestMissingPermissionsIfNeeded();
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        saveJetKvmUrlFromIntent(intent);
        savePairRequestFromIntent(intent);
        if (jetkvmUrlsInput != null) {
            jetkvmUrlsInput.setText("");
        }
        refreshPairingControls();
        startCompanionServiceFromIntent(intent);
    }

    @Override
    protected void onResume() {
        super.onResume();
        updateArmStatus();
        requestMissingPermissionsIfNeeded();
    }

    private android.view.View createSettingsView() {
        int padding = dp(24);

        ScrollView scroller = new ScrollView(this);
        scroller.setFillViewport(false);
        scroller.setBackgroundColor(JETKVM_BACKGROUND);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setGravity(Gravity.CENTER_HORIZONTAL);
        root.setPadding(padding, dp(44), padding, padding);
        root.setBackgroundColor(JETKVM_BACKGROUND);
        scroller.addView(root, new ScrollView.LayoutParams(
            ScrollView.LayoutParams.MATCH_PARENT,
            ScrollView.LayoutParams.WRAP_CONTENT
        ));

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
        jetkvmUrlsLabel.setText("JetKVM endpoint");
        jetkvmUrlsLabel.setTextColor(Color.WHITE);
        jetkvmUrlsLabel.setTextSize(16);
        root.addView(jetkvmUrlsLabel, tightWrap());

        jetkvmUrlsInput = new EditText(this);
        jetkvmUrlsInput.setSingleLine(true);
        jetkvmUrlsInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        jetkvmUrlsInput.setText("");
        jetkvmUrlsInput.setHint("192.168.8.229 or http://jetkvm.local");
        jetkvmUrlsInput.setTextColor(Color.WHITE);
        jetkvmUrlsInput.setHintTextColor(Color.rgb(148, 163, 184));
        jetkvmUrlsInput.addTextChangedListener(new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
                updatePairButtonState();
            }

            @Override
            public void afterTextChanged(Editable s) {
            }
        });
        root.addView(jetkvmUrlsInput, matchWrap());

        LinearLayout pairActionRow = new LinearLayout(this);
        pairActionRow.setOrientation(LinearLayout.HORIZONTAL);
        pairActionRow.setGravity(Gravity.CENTER_VERTICAL);

        pairJetkvmButton = new Button(this);
        pairJetkvmButton.setText("Pair");
        pairJetkvmButton.setAllCaps(false);
        applyButtonStyle(pairJetkvmButton);
        pairJetkvmButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                String value = jetkvmUrlsInput.getText().toString();
                if (value.trim().length() == 0) {
                    updateStatus("Enter a JetKVM endpoint to pair.");
                    return;
                }
                pairJetKvmEndpoint(value);
            }
        });
        pairActionRow.addView(pairJetkvmButton, buttonWrap());

        pairJetkvmState = new TextView(this);
        pairJetkvmState.setText("Paired");
        pairJetkvmState.setTextColor(Color.rgb(34, 197, 94));
        pairJetkvmState.setTextSize(14);
        pairJetkvmState.setPadding(dp(12), 0, 0, 0);
        pairActionRow.addView(pairJetkvmState, new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ));
        root.addView(pairActionRow, matchWrap());

        pairingsList = new LinearLayout(this);
        pairingsList.setOrientation(LinearLayout.VERTICAL);
        root.addView(pairingsList, matchWrap());
        refreshPairingControls();
        updatePairButtonState();

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

        return scroller;
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

    private LinearLayout.LayoutParams buttonWrap() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.WRAP_CONTENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        );
        params.setMargins(0, 0, 0, 0);
        return params;
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void applyButtonStyle(Button button) {
        button.setTextColor(Color.WHITE);
        button.setBackgroundTintList(ColorStateList.valueOf(JETKVM_BLUE_700));
    }

    private void applyDisabledButtonStyle(Button button) {
        button.setTextColor(Color.rgb(203, 213, 225));
        button.setBackgroundTintList(ColorStateList.valueOf(Color.rgb(51, 65, 85)));
    }

    private void updateStatus(String message) {
        if (statusText != null) statusText.setText(message);
    }

    private void updatePairButtonState() {
        if (pairJetkvmButton == null || pairJetkvmState == null || jetkvmUrlsInput == null) {
            return;
        }
        String entered = jetkvmUrlsInput.getText().toString().trim();
        boolean hasValue = entered.length() > 0;
        boolean paired = hasValue && CompanionService.getPairing(prefs, entered) != null;
        pairJetkvmButton.setEnabled(hasValue && !paired);
        if (paired) {
            applyDisabledButtonStyle(pairJetkvmButton);
            pairJetkvmState.setVisibility(android.view.View.VISIBLE);
        } else {
            applyButtonStyle(pairJetkvmButton);
            pairJetkvmState.setVisibility(android.view.View.GONE);
        }
    }

    private void saveJetKvmUrlFromIntent(Intent intent) {
        if (intent == null || !intent.hasExtra(EXTRA_JETKVM_URL)) return;

        String value = intent.getStringExtra(EXTRA_JETKVM_URL);
        if (value == null) return;

        value = value.trim();
        if (value.length() == 0) value = CompanionService.DEFAULT_JETKVM_URL;
        CompanionService.addJetKvmUrl(prefs, value);
    }

    private void savePairRequestFromIntent(Intent intent) {
        if (intent == null || !intent.hasExtra(EXTRA_PAIR_REQUEST_ID)) return;
        String requestId = intent.getStringExtra(EXTRA_PAIR_REQUEST_ID);
        String url = intent.getStringExtra(EXTRA_JETKVM_URL);
        if (requestId == null || url == null) return;
        pendingPairRequestId = requestId;
        pendingPairUrl = normalizeJetKvmUrl(url);
        updateStatus("Pairing request from " + pendingPairUrl + ".");
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

    private void refreshPairingControls() {
        if (pairingsList == null) return;
        pairingsList.removeAllViews();

        if (pendingPairUrl.length() > 0 && pendingPairRequestId.length() > 0) {
            addIncomingPairingControls();
        }
        CompanionService.CompanionPairing[] pairings = CompanionService.getSavedPairings(prefs);
        if (pairings.length == 0) {
            TextView empty = new TextView(this);
            empty.setText("No paired JetKVM endpoints.");
            empty.setTextColor(Color.rgb(148, 163, 184));
            empty.setTextSize(13);
            pairingsList.addView(empty, tightWrap());
        }
        for (final CompanionService.CompanionPairing pairing : pairings) {
            final String url = pairing.url;
            LinearLayout row = new LinearLayout(this);
            row.setOrientation(LinearLayout.HORIZONTAL);
            row.setGravity(Gravity.CENTER_VERTICAL);
            row.setPadding(0, dp(4), 0, dp(4));

            Button unpairButton = new Button(this);
            unpairButton.setText("Unpair");
            unpairButton.setAllCaps(false);
            applyButtonStyle(unpairButton);
            unpairButton.setTextSize(12);
            unpairButton.setMinHeight(0);
            unpairButton.setMinimumHeight(0);
            unpairButton.setPadding(dp(10), 0, dp(10), 0);
            unpairButton.setOnClickListener(new android.view.View.OnClickListener() {
                @Override
                public void onClick(android.view.View v) {
                    unpairJetKvmEndpoint(url);
                }
            });
            row.addView(unpairButton, buttonWrap());

            TextView label = new TextView(this);
            label.setText(url);
            label.setTextColor(Color.WHITE);
            label.setTextSize(14);
            label.setPadding(dp(12), 0, dp(8), 0);
            row.addView(label, new LinearLayout.LayoutParams(
                0,
                LinearLayout.LayoutParams.WRAP_CONTENT,
                1
            ));

            TextView state = new TextView(this);
            state.setTextSize(13);
            state.setTextColor(Color.rgb(34, 197, 94));
            state.setText("Paired");
            row.addView(state, buttonWrap());

            pairingsList.addView(row, matchWrap());
        }
        updatePairButtonState();
    }

    private void addIncomingPairingControls() {
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.VERTICAL);
        row.setPadding(0, 0, 0, dp(12));

        TextView label = new TextView(this);
        label.setText("Pairing request from " + pendingPairUrl);
        label.setTextColor(Color.WHITE);
        label.setTextSize(14);
        row.addView(label, tightWrap());

        final EditText otpInput = new EditText(this);
        otpInput.setSingleLine(true);
        otpInput.setInputType(InputType.TYPE_CLASS_NUMBER);
        otpInput.setHint("6 digit code shown in JetKVM web UI");
        otpInput.setTextColor(Color.WHITE);
        otpInput.setHintTextColor(Color.rgb(148, 163, 184));
        row.addView(otpInput, matchWrap());

        Button acceptButton = new Button(this);
        acceptButton.setText("Pair");
        acceptButton.setAllCaps(false);
        applyButtonStyle(acceptButton);
        acceptButton.setOnClickListener(new android.view.View.OnClickListener() {
            @Override
            public void onClick(android.view.View v) {
                completeJetKvmInitiatedPairing(pendingPairUrl, pendingPairRequestId, otpInput.getText().toString());
            }
        });
        row.addView(acceptButton, tightWrap());

        pairingsList.addView(row, matchWrap());
    }

    private void pairJetKvmEndpoint(final String baseUrl) {
        final String otp = generatePairingOtp();
        updateStatus("Pairing " + baseUrl + ". Type " + otp + " in the JetKVM web UI.");
        new Thread(new Runnable() {
            @Override
            public void run() {
                final PairResult result = requestPairing(baseUrl, otp);
                runOnUiThread(new Runnable() {
                    @Override
                    public void run() {
                        if (result.success) {
                            CompanionService.savePairing(prefs, baseUrl, result.token, result.identityToken);
                            refreshPairingControls();
                            startForegroundService(new Intent(MainActivity.this, CompanionService.class));
                            updateStatus("Paired " + baseUrl + ".");
                        } else {
                            updateStatus("Pairing failed for " + baseUrl + ": " + result.message);
                        }
                    }
                });
            }
        }, "JetKVM-pair").start();
    }

    private PairResult requestPairing(String baseUrl, String otp) {
        HttpURLConnection conn = null;
        try {
            String trimmedBaseUrl = normalizeJetKvmUrl(baseUrl);
            URL url = new URL(trimmedBaseUrl + "/companion/pair");
            conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(3000);
            conn.setReadTimeout(3000);
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.setRequestProperty("Content-Type", "application/json");
            byte[] requestBody = ("{\"otp\":\"" + otp + "\"}").getBytes(StandardCharsets.UTF_8);
            conn.setFixedLengthStreamingMode(requestBody.length);
            conn.getOutputStream().write(requestBody);

            int status = conn.getResponseCode();
            String response = readAll(status >= 400 ? conn.getErrorStream() : conn.getInputStream());
            if (status == 200) {
                return parsePairingResponse(response);
            }
            if (status != 202) {
                return PairResult.error("HTTP " + status);
            }

            String requestId = extractJsonString(response, "request_id");
            if (requestId.length() == 0) {
                return PairResult.error("missing request id");
            }
            return pollPairingStatus(trimmedBaseUrl, requestId);
        } catch (Exception e) {
            return PairResult.error(e.getClass().getSimpleName());
        } finally {
            if (conn != null) conn.disconnect();
        }
    }

    private void completeJetKvmInitiatedPairing(final String baseUrl, final String requestId, final String otp) {
        updateStatus("Completing pairing with " + baseUrl + ".");
        new Thread(new Runnable() {
            @Override
            public void run() {
                final PairResult result = claimPairing(baseUrl, requestId, otp);
                runOnUiThread(new Runnable() {
                    @Override
                    public void run() {
                        if (result.success) {
                            CompanionService.savePairing(prefs, baseUrl, result.token, result.identityToken);
                            pendingPairUrl = "";
                            pendingPairRequestId = "";
                            refreshPairingControls();
                            startForegroundService(new Intent(MainActivity.this, CompanionService.class));
                            updateStatus("Paired " + baseUrl + ".");
                        } else {
                            updateStatus("Pairing failed for " + baseUrl + ": " + result.message);
                        }
                    }
                });
            }
        }, "JetKVM-pair-claim").start();
    }

    private PairResult claimPairing(String baseUrl, String requestId, String otp) {
        HttpURLConnection conn = null;
        try {
            String trimmedBaseUrl = normalizeJetKvmUrl(baseUrl);
            URL url = new URL(trimmedBaseUrl + "/companion/pair/" + requestId + "/claim");
            conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(3000);
            conn.setReadTimeout(3000);
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.setRequestProperty("Content-Type", "application/json");
            byte[] requestBody = ("{\"otp\":\"" + otp.trim() + "\"}").getBytes(StandardCharsets.UTF_8);
            conn.setFixedLengthStreamingMode(requestBody.length);
            conn.getOutputStream().write(requestBody);
            int status = conn.getResponseCode();
            String response = readAll(status >= 400 ? conn.getErrorStream() : conn.getInputStream());
            if (status == 200) {
                return parsePairingResponse(response);
            }
            return PairResult.error("HTTP " + status);
        } catch (Exception e) {
            return PairResult.error(e.getClass().getSimpleName());
        } finally {
            if (conn != null) conn.disconnect();
        }
    }

    private void unpairJetKvmEndpoint(final String baseUrl) {
        updateStatus("Unpairing " + baseUrl + ".");
        new Thread(new Runnable() {
            @Override
            public void run() {
                final boolean ok = requestUnpair(baseUrl);
                runOnUiThread(new Runnable() {
                    @Override
                    public void run() {
                        if (ok) {
                            CompanionService.removePairing(prefs, baseUrl);
                            refreshPairingControls();
                            startForegroundService(new Intent(MainActivity.this, CompanionService.class));
                            updateStatus("Unpaired " + baseUrl + ".");
                        } else {
                            updateStatus("Unpair failed for " + baseUrl + ".");
                        }
                    }
                });
            }
        }, "JetKVM-unpair").start();
    }

    private boolean requestUnpair(String baseUrl) {
        HttpURLConnection conn = null;
        try {
            String trimmedBaseUrl = normalizeJetKvmUrl(baseUrl);
            String token = CompanionService.getPairingToken(prefs, trimmedBaseUrl);
            URL url = new URL(trimmedBaseUrl + "/companion/unpair");
            conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(3000);
            conn.setReadTimeout(3000);
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.setRequestProperty("X-JetKVM-Companion-Token", token);
            conn.setFixedLengthStreamingMode(0);
            int status = conn.getResponseCode();
            return status == 200 || status == 401 || status == 404;
        } catch (Exception e) {
            return false;
        } finally {
            if (conn != null) conn.disconnect();
        }
    }

    private PairResult pollPairingStatus(String baseUrl, String requestId) {
        for (int i = 0; i < 60; i++) {
            HttpURLConnection conn = null;
            try {
                Thread.sleep(2000);
                URL url = new URL(baseUrl + "/companion/pair/" + requestId);
                conn = (HttpURLConnection) url.openConnection();
                conn.setConnectTimeout(3000);
                conn.setReadTimeout(3000);
                conn.setRequestMethod("GET");
                int status = conn.getResponseCode();
                String response = readAll(status >= 400 ? conn.getErrorStream() : conn.getInputStream());
                if (status == 200 && "paired".equals(extractJsonString(response, "status"))) {
                    return parsePairingResponse(response);
                }
                if (status == 200 && "rejected".equals(extractJsonString(response, "status"))) {
                    return PairResult.error("rejected on JetKVM");
                }
            } catch (Exception e) {
                return PairResult.error(e.getClass().getSimpleName());
            } finally {
                if (conn != null) conn.disconnect();
            }
        }
        return PairResult.error("approval timed out");
    }

    private PairResult parsePairingResponse(String response) {
        String token = extractJsonString(response, "token");
        String identityToken = extractJsonString(response, "jetkvm_identity_token");
        if (token.length() == 0 || identityToken.length() == 0) {
            return PairResult.error("missing token or identity");
        }
        return PairResult.success(token, identityToken);
    }

    private static String generatePairingOtp() {
        return String.format("%06d", PAIRING_RANDOM.nextInt(1000000));
    }

    private static String normalizeJetKvmUrl(String rawUrl) {
        String url = rawUrl == null ? "" : rawUrl.trim();
        if (url.length() == 0) return CompanionService.DEFAULT_JETKVM_URL;
        if (!url.contains("://")) {
            url = "http://" + url;
        }
        while (url.endsWith("/") && url.length() > "http://".length()) {
            url = url.substring(0, url.length() - 1);
        }
        return url;
    }

    private static String readAll(InputStream input) throws java.io.IOException {
        if (input == null) return "";
        BufferedReader reader = new BufferedReader(new InputStreamReader(input, StandardCharsets.UTF_8));
        StringBuilder builder = new StringBuilder();
        String line;
        while ((line = reader.readLine()) != null) {
            builder.append(line);
        }
        reader.close();
        return builder.toString();
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

    private static final class PairResult {
        final boolean success;
        final String token;
        final String identityToken;
        final String message;

        private PairResult(boolean success, String token, String identityToken, String message) {
            this.success = success;
            this.token = token;
            this.identityToken = identityToken;
            this.message = message;
        }

        static PairResult success(String token, String identityToken) {
            return new PairResult(true, token, identityToken, "");
        }

        static PairResult error(String message) {
            return new PairResult(false, "", "", message);
        }
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
