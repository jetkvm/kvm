package com.jetkvm.controller;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.net.http.SslError;
import android.os.Build;
import android.os.Bundle;
import android.os.PowerManager;
import android.text.InputType;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.Window;
import android.view.WindowManager;
import android.view.autofill.AutofillManager;
import android.view.inputmethod.InputMethodManager;
import android.webkit.ConsoleMessage;
import android.webkit.CookieManager;
import android.webkit.JavascriptInterface;
import android.webkit.PermissionRequest;
import android.webkit.SslErrorHandler;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.Toast;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.List;
import java.util.Map;

public class MainActivity extends Activity {
    private static final String PREFS = "jetkvm_android";
    private static final String KEY_URL = "controllerUrl";
    private static final String KEY_STAY_LOGGED_IN = "stayLoggedIn";
    private static final String DEFAULT_URL = "http://jetkvm.local/?jetkvmAndroid=1";
    private static final long WAKE_LOCK_TIMEOUT_MS = 60 * 60 * 1000L;

    private WebView webView;
    private LinearLayout loginPanel;
    private EditText urlInput;
    private EditText usernameInput;
    private EditText passwordInput;
    private CheckBox stayLoggedInInput;
    private Button loginButton;
    private TextView statusText;
    private ProgressBar progressBar;
    private SharedPreferences prefs;
    private PowerManager.WakeLock wakeLock;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        prefs = getSharedPreferences(PREFS, MODE_PRIVATE);

        requestWindowFeature(Window.FEATURE_NO_TITLE);
        getWindow().setFlags(WindowManager.LayoutParams.FLAG_FULLSCREEN, WindowManager.LayoutParams.FLAG_FULLSCREEN);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        PowerManager powerManager = (PowerManager) getSystemService(Context.POWER_SERVICE);
        wakeLock = powerManager.newWakeLock(PowerManager.SCREEN_DIM_WAKE_LOCK, "JetKVM:Controller");

        webView = new WebView(this);
        webView.setBackgroundColor(Color.BLACK);
        configureWebView(webView);

        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        root.addView(webView, new FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.MATCH_PARENT
        ));

        loginPanel = createLoginPanel();
        root.addView(loginPanel, new FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.MATCH_PARENT
        ));
        setContentView(root);

        webView.setOnLongClickListener(new View.OnLongClickListener() {
            @Override
            public boolean onLongClick(View v) {
                return true;
            }
        });
        webView.setHapticFeedbackEnabled(false);

        enterImmersiveMode();
        showLoginPanel("");
    }

    @Override
    protected void onResume() {
        super.onResume();
        enterImmersiveMode();
        if (wakeLock != null && !wakeLock.isHeld()) wakeLock.acquire(WAKE_LOCK_TIMEOUT_MS);
    }

    @Override
    protected void onPause() {
        CookieManager.getInstance().flush();
        if (wakeLock != null && wakeLock.isHeld()) wakeLock.release();
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        if (webView != null) {
            webView.destroy();
            webView = null;
        }
        super.onDestroy();
    }

    @Override
    public void onBackPressed() {
        if (loginPanel != null && loginPanel.getVisibility() == View.VISIBLE) {
            super.onBackPressed();
            return;
        }
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
            return;
        }
        showLoginPanel("Change JetKVM URL or log in again.");
    }

    private LinearLayout createLoginPanel() {
        int padding = dp(24);

        LinearLayout outer = new LinearLayout(this);
        outer.setOrientation(LinearLayout.VERTICAL);
        outer.setGravity(Gravity.CENTER);
        outer.setPadding(padding, padding, padding, padding);
        outer.setBackgroundColor(Color.rgb(7, 12, 28));

        LinearLayout form = new LinearLayout(this);
        form.setOrientation(LinearLayout.VERTICAL);
        form.setGravity(Gravity.CENTER_HORIZONTAL);
        form.setPadding(0, 0, 0, 0);
        outer.addView(form, new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ));

        ImageView logo = new ImageView(this);
        logo.setImageResource(getApplicationInfo().icon);
        logo.setAdjustViewBounds(true);
        LinearLayout.LayoutParams logoParams = new LinearLayout.LayoutParams(dp(72), dp(72));
        logoParams.setMargins(0, 0, 0, dp(14));
        form.addView(logo, logoParams);

        TextView title = new TextView(this);
        title.setText("JetKVM");
        title.setTextColor(Color.WHITE);
        title.setTextSize(28);
        title.setGravity(Gravity.CENTER);
        form.addView(title, new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ));

        statusText = new TextView(this);
        statusText.setTextColor(Color.rgb(148, 163, 184));
        statusText.setTextSize(14);
        statusText.setGravity(Gravity.CENTER);
        statusText.setPadding(0, dp(8), 0, dp(16));
        form.addView(statusText, new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ));

        urlInput = new EditText(this);
        urlInput.setSingleLine(true);
        urlInput.setText(normalizeUrl(prefs.getString(KEY_URL, DEFAULT_URL)));
        urlInput.setHint("JetKVM URL");
        urlInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        urlInput.setSelectAllOnFocus(true);
        form.addView(urlInput, fieldLayoutParams());

        usernameInput = new EditText(this);
        usernameInput.setSingleLine(true);
        usernameInput.setText("JetKVM");
        usernameInput.setHint("Username");
        usernameInput.setInputType(InputType.TYPE_CLASS_TEXT);
        enableAutofill(usernameInput, View.AUTOFILL_HINT_USERNAME);
        usernameInput.setSelectAllOnFocus(true);
        form.addView(usernameInput, fieldLayoutParams());

        passwordInput = new EditText(this);
        passwordInput.setSingleLine(true);
        passwordInput.setHint("Password");
        passwordInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        enableAutofill(passwordInput, View.AUTOFILL_HINT_PASSWORD);
        form.addView(passwordInput, fieldLayoutParams());

        stayLoggedInInput = new CheckBox(this);
        stayLoggedInInput.setText("Stay logged in");
        stayLoggedInInput.setTextColor(Color.WHITE);
        stayLoggedInInput.setChecked(prefs.getBoolean(KEY_STAY_LOGGED_IN, true));
        form.addView(stayLoggedInInput, new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ));

        loginButton = new Button(this);
        loginButton.setText("Log in");
        loginButton.setAllCaps(false);
        loginButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                submitNativeLogin(
                    urlInput.getText().toString(),
                    passwordInput.getText().toString(),
                    stayLoggedInInput.isChecked()
                );
            }
        });
        form.addView(loginButton, fieldLayoutParams());

        progressBar = new ProgressBar(this);
        progressBar.setVisibility(View.GONE);
        form.addView(progressBar, new LinearLayout.LayoutParams(dp(40), dp(40)));

        return outer;
    }

    private LinearLayout.LayoutParams fieldLayoutParams() {
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

    private void enableAutofill(View view, String hint) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            view.setAutofillHints(hint);
            view.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_YES);
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    private void configureWebView(WebView view) {
        WebView.setWebContentsDebuggingEnabled(false);

        WebSettings settings = view.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setDatabaseEnabled(true);
        settings.setSaveFormData(false);
        settings.setSavePassword(false);
        settings.setMediaPlaybackRequiresUserGesture(false);
        settings.setLoadWithOverviewMode(false);
        settings.setUseWideViewPort(false);
        settings.setSupportZoom(false);
        settings.setBuiltInZoomControls(false);
        settings.setDisplayZoomControls(false);
        settings.setAllowFileAccess(false);
        settings.setAllowContentAccess(false);
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE);
        settings.setUserAgentString(settings.getUserAgentString() + " JetKVMWebView/1");

        CookieManager cookieManager = CookieManager.getInstance();
        cookieManager.setAcceptCookie(true);
        cookieManager.setAcceptThirdPartyCookies(view, true);

        view.addJavascriptInterface(new JetKVMBridge(), "JetKVMAndroid");
        view.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS);
        view.setOverScrollMode(View.OVER_SCROLL_NEVER);
        view.setScrollBarStyle(View.SCROLLBARS_INSIDE_OVERLAY);
        view.setVerticalScrollBarEnabled(false);
        view.setHorizontalScrollBarEnabled(false);

        view.setWebChromeClient(new WebChromeClient() {
            @Override
            public void onPermissionRequest(final PermissionRequest request) {
                runOnUiThread(new Runnable() {
                    @Override
                    public void run() {
                        request.grant(request.getResources());
                    }
                });
            }

            @Override
            public boolean onConsoleMessage(ConsoleMessage consoleMessage) {
                return true;
            }
        });

        view.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                return false;
            }

            @Override
            public void onPageFinished(WebView view, String url) {
                injectJetKVMHooks(view);
                CookieManager.getInstance().flush();
                if (url.contains("/login-local")) {
                    showLoginPanel("Session expired. Log in again.");
                }
            }

            @Override
            public void onReceivedHttpError(
                WebView view,
                WebResourceRequest request,
                WebResourceResponse errorResponse
            ) {
                if (request.isForMainFrame()) {
                    showLoginPanel("JetKVM returned an HTTP error.");
                }
            }

            @Override
            public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
                if (request.isForMainFrame()) {
                    showLoginPanel("Unable to load JetKVM. Check the URL.");
                }
            }

            @Override
            public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
                handler.cancel();
                showLoginPanel("JetKVM certificate is not trusted.");
            }
        });

        view.setOnTouchListener(new View.OnTouchListener() {
            @Override
            public boolean onTouch(View v, MotionEvent event) {
                if (event.getAction() == MotionEvent.ACTION_DOWN) enterImmersiveMode();
                return false;
            }
        });
    }

    private void showController() {
        hideKeyboard();
        loginPanel.setVisibility(View.GONE);
        webView.setVisibility(View.VISIBLE);
        webView.loadUrl(normalizeUrl(prefs.getString(KEY_URL, DEFAULT_URL)));
        enterImmersiveMode();
    }

    private void showLoginPanel(String message) {
        if (loginPanel == null) return;
        webView.setVisibility(View.GONE);
        loginPanel.setVisibility(View.VISIBLE);
        setBusy(false);
        statusText.setText(message == null ? "" : message);
        urlInput.setText(normalizeUrl(prefs.getString(KEY_URL, urlInput.getText().toString())));
        passwordInput.setText("");
        passwordInput.requestFocus();
        InputMethodManager imm = (InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE);
        if (imm != null) imm.showSoftInput(passwordInput, InputMethodManager.SHOW_IMPLICIT);
        requestPasswordAutofill();
        enterImmersiveMode();
    }

    private void requestPasswordAutofill() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O || passwordInput == null) return;

        passwordInput.postDelayed(new Runnable() {
            @Override
            public void run() {
                AutofillManager autofillManager =
                    (AutofillManager) getSystemService(AutofillManager.class);
                if (autofillManager != null) {
                    autofillManager.cancel();
                    autofillManager.requestAutofill(passwordInput);
                }
            }
        }, 250);
    }

    private void commitAutofillSession() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return;

        AutofillManager autofillManager =
            (AutofillManager) getSystemService(AutofillManager.class);
        if (autofillManager != null) autofillManager.commit();
    }

    private void setBusy(boolean busy) {
        if (loginButton == null) return;
        loginButton.setEnabled(!busy);
        progressBar.setVisibility(busy ? View.VISIBLE : View.GONE);
    }

    private String normalizeUrl(String value) {
        String url = value == null ? DEFAULT_URL : value.trim();
        if (url.isEmpty()) url = DEFAULT_URL;
        if (!url.startsWith("http://") && !url.startsWith("https://")) {
            url = "http://" + url;
        }
        if (!url.contains("?")) {
            url += "?jetkvmAndroid=1";
        } else if (!url.contains("jetkvmAndroid=1")) {
            url += "&jetkvmAndroid=1";
        }
        return url;
    }

    private boolean isAllowedControllerUrl(String value) {
        try {
            URL url = new URL(normalizeUrl(value));
            String protocol = url.getProtocol();
            if ("https".equalsIgnoreCase(protocol)) return true;
            if (!"http".equalsIgnoreCase(protocol)) return false;

            String host = url.getHost();
            if (host == null) return false;
            host = host.toLowerCase();

            return "localhost".equals(host)
                || host.endsWith(".local")
                || isPrivateIpv4(host);
        } catch (Exception e) {
            return false;
        }
    }

    private boolean isPrivateIpv4(String host) {
        String[] parts = host.split("\\.");
        if (parts.length != 4) return false;

        int[] octets = new int[4];
        for (int i = 0; i < parts.length; i++) {
            try {
                octets[i] = Integer.parseInt(parts[i]);
            } catch (NumberFormatException e) {
                return false;
            }
            if (octets[i] < 0 || octets[i] > 255) return false;
        }

        return octets[0] == 10
            || (octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31)
            || (octets[0] == 192 && octets[1] == 168)
            || (octets[0] == 169 && octets[1] == 254)
            || octets[0] == 127;
    }

    private void submitNativeLogin(final String controllerUrl, final String password, final boolean stayLoggedIn) {
        final String normalizedUrl = normalizeUrl(controllerUrl);
        if (!isAllowedControllerUrl(normalizedUrl)) {
            showLoginFailed("Use HTTPS or a local JetKVM address.");
            return;
        }

        prefs.edit().putString(KEY_URL, normalizedUrl).apply();
        setBusy(true);
        statusText.setText("Logging in...");

        new Thread(new Runnable() {
            @Override
            public void run() {
                try {
                    final String origin = getOrigin(normalizedUrl);
                    URL url = new URL(origin + "/auth/login-local");
                    HttpURLConnection conn = (HttpURLConnection) url.openConnection();
                    conn.setRequestMethod("POST");
                    conn.setConnectTimeout(5000);
                    conn.setReadTimeout(5000);
                    conn.setDoOutput(true);
                    conn.setRequestProperty("Content-Type", "application/json");

                    String body = "{\"password\":\"" + jsonEscape(password) + "\",\"stayLoggedIn\":"
                        + (stayLoggedIn ? "true" : "false") + "}";
                    OutputStream out = conn.getOutputStream();
                    out.write(body.getBytes("UTF-8"));
                    out.close();

                    int status = conn.getResponseCode();
                    if (status < 200 || status >= 300) {
                        showLoginFailed("Invalid password or login failed.");
                        return;
                    }

                    Map<String, List<String>> headers = conn.getHeaderFields();
                    for (Map.Entry<String, List<String>> entry : headers.entrySet()) {
                        if (entry.getKey() == null || !"Set-Cookie".equalsIgnoreCase(entry.getKey())) continue;
                        for (String cookie : entry.getValue()) {
                            CookieManager.getInstance().setCookie(origin, cookie);
                        }
                    }
                    CookieManager.getInstance().flush();
                    prefs.edit()
                        .putString(KEY_URL, normalizedUrl)
                        .putBoolean(KEY_STAY_LOGGED_IN, stayLoggedIn)
                        .apply();

                    runOnUiThread(new Runnable() {
                        @Override
                        public void run() {
                            commitAutofillSession();
                            setBusy(false);
                            showController();
                        }
                    });
                } catch (Exception e) {
                    showLoginFailed("Unable to reach JetKVM. Check the URL.");
                }
            }
        }).start();
    }

    private void showLoginFailed(final String message) {
        runOnUiThread(new Runnable() {
            @Override
            public void run() {
                Toast.makeText(MainActivity.this, message, Toast.LENGTH_SHORT).show();
                setBusy(false);
                statusText.setText(message);
                passwordInput.requestFocus();
            }
        });
    }

    private String getOrigin(String value) throws Exception {
        URL url = new URL(normalizeUrl(value));
        String origin = url.getProtocol() + "://" + url.getHost();
        if (url.getPort() != -1) origin += ":" + url.getPort();
        return origin;
    }

    private String jsonEscape(String value) {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
            .replace("\t", "\\t");
    }

    private void hideKeyboard() {
        InputMethodManager imm = (InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE);
        View tokenView = loginPanel != null && loginPanel.getVisibility() == View.VISIBLE ? loginPanel : webView;
        if (imm != null && tokenView != null) {
            imm.hideSoftInputFromWindow(tokenView.getWindowToken(), 0);
        }
    }

    private void enterImmersiveMode() {
        View decor = getWindow().getDecorView();
        decor.setSystemUiVisibility(
            View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                | View.SYSTEM_UI_FLAG_FULLSCREEN
                | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
        );
    }

    private void injectJetKVMHooks(WebView view) {
        view.evaluateJavascript(
            "(function(){"
                + "if(window.__jetkvmAndroidHooksInstalled)return;"
                + "window.__jetkvmAndroidHooksInstalled=true;"
                + "function isEditable(el){"
                    + "if(!el)return false;"
                    + "var tag=(el.tagName||'').toLowerCase();"
                    + "return tag==='input'||tag==='textarea'||tag==='select'||el.isContentEditable;"
                + "}"
                + "document.addEventListener('pointerdown',function(e){"
                    + "if(isEditable(e.target))return;"
                    + "if(isEditable(document.activeElement))document.activeElement.blur();"
                    + "window.JetKVMAndroid.hideKeyboard();"
                + "},true);"
            + "})();",
            null
        );
    }

    private final class JetKVMBridge {
        @JavascriptInterface
        public void setStayLoggedIn(boolean stayLoggedIn) {
            prefs.edit().putBoolean(KEY_STAY_LOGGED_IN, stayLoggedIn).apply();
        }

        @JavascriptInterface
        public void showNativeLogin(String url) {
            runOnUiThread(new Runnable() {
                @Override
                public void run() {
                    showLoginPanel("Session expired. Log in again.");
                }
            });
        }

        @JavascriptInterface
        public void hideKeyboard() {
            runOnUiThread(new Runnable() {
                @Override
                public void run() {
                    MainActivity.this.hideKeyboard();
                }
            });
        }
    }
}
