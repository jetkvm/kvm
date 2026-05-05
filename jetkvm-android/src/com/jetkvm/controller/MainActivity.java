package com.jetkvm.controller;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.Context;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.net.http.SslError;
import android.os.Bundle;
import android.os.PowerManager;
import android.text.InputType;
import android.view.MotionEvent;
import android.view.View;
import android.view.Window;
import android.view.WindowManager;
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
import android.content.DialogInterface;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
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

    private WebView webView;
    private SharedPreferences prefs;
    private PowerManager.WakeLock wakeLock;
    private boolean urlDialogShowing;
    private boolean loginDialogShowing;

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
        setContentView(root);

        webView.setOnLongClickListener(new View.OnLongClickListener() {
            @Override
            public boolean onLongClick(View v) {
                showUrlDialog();
                return true;
            }
        });
        webView.setHapticFeedbackEnabled(true);

        enterImmersiveMode();
        loadControllerAfterCookieSetup();
    }

    @Override
    protected void onResume() {
        super.onResume();
        enterImmersiveMode();
        if (wakeLock != null && !wakeLock.isHeld()) wakeLock.acquire();
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
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
            return;
        }
        super.onBackPressed();
    }

    @SuppressLint("SetJavaScriptEnabled")
    private void configureWebView(WebView view) {
        WebView.setWebContentsDebuggingEnabled(false);

        WebSettings settings = view.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setDatabaseEnabled(true);
        settings.setSaveFormData(true);
        settings.setSavePassword(true);
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
        view.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_YES);
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
                    showNativeLoginDialog(url);
                }
            }

            @Override
            public void onReceivedHttpError(
                WebView view,
                WebResourceRequest request,
                WebResourceResponse errorResponse
            ) {
                if (request.isForMainFrame()) {
                    Toast.makeText(MainActivity.this, "JetKVM HTTP error", Toast.LENGTH_SHORT).show();
                    showUrlDialog();
                }
            }

            @Override
            public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
                if (request.isForMainFrame()) {
                    Toast.makeText(MainActivity.this, "Unable to load JetKVM", Toast.LENGTH_SHORT).show();
                    showUrlDialog();
                }
            }

            @Override
            public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
                handler.cancel();
                Toast.makeText(MainActivity.this, "JetKVM certificate is not trusted", Toast.LENGTH_LONG).show();
                showUrlDialog();
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

    private void loadController() {
        webView.loadUrl(normalizeUrl(prefs.getString(KEY_URL, DEFAULT_URL)));
    }

    private void loadControllerAfterCookieSetup() {
        if (prefs.getBoolean(KEY_STAY_LOGGED_IN, false)) {
            loadController();
            return;
        }

        CookieManager.getInstance().removeAllCookies(new ValueCallback<Boolean>() {
            @Override
            public void onReceiveValue(Boolean value) {
                CookieManager.getInstance().flush();
                runOnUiThread(new Runnable() {
                    @Override
                    public void run() {
                        loadController();
                    }
                });
            }
        });
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

    private void showUrlDialog() {
        if (urlDialogShowing) return;
        urlDialogShowing = true;
        enterImmersiveMode();

        EditText input = new EditText(this);
        input.setSingleLine(true);
        input.setText(normalizeUrl(prefs.getString(KEY_URL, DEFAULT_URL)));
        input.setSelectAllOnFocus(true);

        new AlertDialog.Builder(this)
            .setTitle("JetKVM URL")
            .setView(input)
            .setPositiveButton("Load", new DialogInterface.OnClickListener() {
                @Override
                public void onClick(DialogInterface dialog, int which) {
                    String url = normalizeUrl(input.getText().toString());
                    prefs.edit().putString(KEY_URL, url).apply();
                    webView.loadUrl(url);
                    enterImmersiveMode();
                }
            })
            .setNegativeButton("Cancel", new DialogInterface.OnClickListener() {
                @Override
                public void onClick(DialogInterface dialog, int which) {
                    enterImmersiveMode();
                }
            })
            .setNeutralButton("Reset", new DialogInterface.OnClickListener() {
                @Override
                public void onClick(DialogInterface dialog, int which) {
                    prefs.edit().remove(KEY_URL).apply();
                    loadController();
                    enterImmersiveMode();
                }
            })
            .setOnDismissListener(new DialogInterface.OnDismissListener() {
                @Override
                public void onDismiss(DialogInterface dialog) {
                    urlDialogShowing = false;
                }
            })
            .show();
    }

    private void showNativeLoginDialog(final String pageUrl) {
        if (loginDialogShowing) return;
        loginDialogShowing = true;
        enterImmersiveMode();

        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        int padding = Math.round(20 * getResources().getDisplayMetrics().density);
        layout.setPadding(padding, 0, padding, 0);

        EditText usernameInput = new EditText(this);
        usernameInput.setSingleLine(true);
        usernameInput.setText("JetKVM");
        usernameInput.setInputType(InputType.TYPE_CLASS_TEXT);
        usernameInput.setAutofillHints(View.AUTOFILL_HINT_USERNAME);
        usernameInput.setSelectAllOnFocus(true);
        layout.addView(usernameInput);

        final EditText passwordInput = new EditText(this);
        passwordInput.setSingleLine(true);
        passwordInput.setHint("Password");
        passwordInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        passwordInput.setAutofillHints(View.AUTOFILL_HINT_PASSWORD);
        layout.addView(passwordInput);

        final CheckBox stayLoggedInInput = new CheckBox(this);
        stayLoggedInInput.setText("Stay logged in");
        stayLoggedInInput.setChecked(true);
        layout.addView(stayLoggedInInput);

        final AlertDialog dialog = new AlertDialog.Builder(this)
            .setTitle("JetKVM Login")
            .setView(layout)
            .setPositiveButton("Log in", null)
            .setNegativeButton("Use web form", new DialogInterface.OnClickListener() {
                @Override
                public void onClick(DialogInterface dialog, int which) {
                    enterImmersiveMode();
                }
            })
            .setOnDismissListener(new DialogInterface.OnDismissListener() {
                @Override
                public void onDismiss(DialogInterface dialog) {
                    loginDialogShowing = false;
                }
            })
            .create();

        dialog.setOnShowListener(new DialogInterface.OnShowListener() {
            @Override
            public void onShow(DialogInterface dialogInterface) {
                dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(new View.OnClickListener() {
                    @Override
                    public void onClick(View v) {
                        submitNativeLogin(
                            pageUrl,
                            passwordInput.getText().toString(),
                            stayLoggedInInput.isChecked()
                        );
                        dialog.dismiss();
                    }
                });
                passwordInput.requestFocus();
                dialog.getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_VISIBLE);
            }
        });
        dialog.show();
    }

    private void submitNativeLogin(final String pageUrl, final String password, final boolean stayLoggedIn) {
        new Thread(new Runnable() {
            @Override
            public void run() {
                try {
                    String origin = getOrigin(pageUrl);
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
                        showNativeLoginFailed(pageUrl);
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
                    prefs.edit().putBoolean(KEY_STAY_LOGGED_IN, stayLoggedIn).apply();

                    final String controllerUrl = origin + "/?jetkvmAndroid=1";
                    runOnUiThread(new Runnable() {
                        @Override
                        public void run() {
                            hideKeyboard();
                            webView.loadUrl(controllerUrl);
                            enterImmersiveMode();
                        }
                    });
                } catch (Exception e) {
                    showNativeLoginFailed(pageUrl);
                }
            }
        }).start();
    }

    private void showNativeLoginFailed(final String pageUrl) {
        runOnUiThread(new Runnable() {
            @Override
            public void run() {
                Toast.makeText(MainActivity.this, "JetKVM login failed", Toast.LENGTH_SHORT).show();
                showNativeLoginDialog(pageUrl);
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
        if (imm != null && webView != null) {
            imm.hideSoftInputFromWindow(webView.getWindowToken(), 0);
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
                + "function installLoginHook(){"
                    + "var form=document.querySelector('form');"
                    + "var box=document.querySelector('input[name=\"stayLoggedIn\"]');"
                    + "var password=document.querySelector('input[name=\"password\"]');"
                    + "if(form&&password&&!window.__jetkvmNativeLoginRequested){"
                        + "window.__jetkvmNativeLoginRequested=true;"
                        + "setTimeout(function(){window.JetKVMAndroid.showNativeLogin(location.href);},100);"
                    + "}"
                    + "if(!form||!box||form.__jetkvmStayHook)return;"
                    + "form.__jetkvmStayHook=true;"
                    + "form.addEventListener('submit',function(){"
                        + "window.JetKVMAndroid.setStayLoggedIn(!!box.checked);"
                        + "if(isEditable(document.activeElement))document.activeElement.blur();"
                        + "window.JetKVMAndroid.hideKeyboard();"
                    + "},true);"
                + "}"
                + "function isEditable(el){"
                    + "if(!el)return false;"
                    + "var tag=(el.tagName||'').toLowerCase();"
                    + "return tag==='input'||tag==='textarea'||tag==='select'||el.isContentEditable;"
                + "}"
                + "if(!document.__jetkvmImeHook){"
                    + "document.__jetkvmImeHook=true;"
                    + "document.addEventListener('pointerdown',function(e){"
                        + "if(isEditable(e.target))return;"
                        + "if(isEditable(document.activeElement))document.activeElement.blur();"
                        + "window.JetKVMAndroid.hideKeyboard();"
                    + "},true);"
                + "}"
                + "installLoginHook();"
                + "setInterval(function(){installLoginHook();},1000);"
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
        public void showNativeLogin(final String url) {
            runOnUiThread(new Runnable() {
                @Override
                public void run() {
                    showNativeLoginDialog(url);
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
