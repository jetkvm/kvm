package com.jetkvm.companion;

import android.app.Activity;
import android.app.KeyguardManager;
import android.content.Context;
import android.content.Intent;
import android.graphics.Color;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import android.view.Window;
import android.view.WindowManager;
import android.widget.FrameLayout;

public class DismissActivity extends Activity {
    static final String ACTION_MANUAL = "com.jetkvm.companion.MANUAL_DISMISS";

    private static final long DISMISS_DELAY_MS = 300;
    private static final long FINISH_TIMEOUT_MS = 2500;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private KeyguardManager keyguardManager;
    private boolean dismissInFlight;
    private boolean dismissScheduled;
    private final Runnable finishTimeout = new Runnable() {
        @Override
        public void run() {
            logState("finish timeout");
            finishAndRemoveTask();
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        requestWindowFeature(Window.FEATURE_NO_TITLE);
        configureLockscreenWindow();

        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.TRANSPARENT);
        setContentView(root);

        keyguardManager = (KeyguardManager) getSystemService(Context.KEYGUARD_SERVICE);
        scheduleDismiss("onCreate");
    }

    @Override
    protected void onResume() {
        super.onResume();
        scheduleDismiss("onResume");
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        scheduleDismiss("onNewIntent:" + intent.getAction());
    }

    private void configureLockscreenWindow() {
        if (android.os.Build.VERSION.SDK_INT >= 27) {
            setShowWhenLocked(true);
            setTurnScreenOn(false);
        } else {
            getWindow().addFlags(WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED);
        }
        getWindow().clearFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND);
    }

    private void scheduleDismiss(final String reason) {
        if (dismissScheduled) return;
        dismissScheduled = true;
        handler.postDelayed(new Runnable() {
            @Override
            public void run() {
                requestDismiss(reason);
            }
        }, DISMISS_DELAY_MS);
    }

    private void requestDismiss(String reason) {
        if (keyguardManager == null || dismissInFlight) return;
        handler.postDelayed(finishTimeout, FINISH_TIMEOUT_MS);

        boolean keyguardLocked = keyguardManager.isKeyguardLocked();
        boolean deviceLocked = keyguardManager.isDeviceLocked();
        Log.i(CompanionService.TAG, reason + " keyguardLocked=" + keyguardLocked + " deviceLocked=" + deviceLocked);

        if (!keyguardLocked) {
            handler.removeCallbacks(finishTimeout);
            finishAndRemoveTask();
            return;
        }

        dismissInFlight = true;
        keyguardManager.requestDismissKeyguard(this, new KeyguardManager.KeyguardDismissCallback() {
            @Override
            public void onDismissError() {
                dismissInFlight = false;
                handler.removeCallbacks(finishTimeout);
                logState("callback onDismissError");
                finishAndRemoveTask();
            }

            @Override
            public void onDismissSucceeded() {
                dismissInFlight = false;
                handler.removeCallbacks(finishTimeout);
                logState("callback onDismissSucceeded");
                finishAndRemoveTask();
            }

            @Override
            public void onDismissCancelled() {
                dismissInFlight = false;
                handler.removeCallbacks(finishTimeout);
                logState("callback onDismissCancelled");
                finishAndRemoveTask();
            }
        });
    }

    private void logState(String label) {
        boolean keyguardLocked = keyguardManager != null && keyguardManager.isKeyguardLocked();
        boolean deviceLocked = keyguardManager != null && keyguardManager.isDeviceLocked();
        Log.i(CompanionService.TAG, label + " keyguardLocked=" + keyguardLocked + " deviceLocked=" + deviceLocked);
    }
}
