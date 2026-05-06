package com.jetkvm.companion;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.graphics.PixelFormat;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.provider.Settings;
import android.util.Log;
import android.view.Gravity;
import android.view.View;
import android.view.WindowManager;

public class CompanionService extends Service {
    static final String TAG = "JetKVMCompanion";
    static final String ACTION_SCREEN_OFF = "com.jetkvm.companion.SCREEN_OFF";
    static final String ACTION_SCREEN_ON = "com.jetkvm.companion.SCREEN_ON";
    static final String PREFS = "jetkvm_companion";
    static final String KEY_LAUNCH_ON_BOOT = "launch_on_boot";

    private static final String CHANNEL_ID = "jetkvm-companion";
    private static final int NOTIFICATION_ID = 1001;

    private WindowManager windowManager;
    private View launchAssistOverlay;
    private final Handler handler = new Handler(Looper.getMainLooper());

    private final BroadcastReceiver screenReceiver = new BroadcastReceiver() {
        @Override
        public void onReceive(Context context, Intent intent) {
            String action = intent.getAction();
            Log.i(TAG, "screen receiver action=" + action);
            if (Intent.ACTION_SCREEN_ON.equals(action)) {
                handler.postDelayed(new Runnable() {
                    @Override
                    public void run() {
                        launchDismissActivity(ACTION_SCREEN_ON);
                    }
                }, 600);
            }
        }
    };

    @Override
    public void onCreate() {
        super.onCreate();
        createChannel();
        startForeground(NOTIFICATION_ID, buildNotification());
        ensureLaunchAssistOverlay();

        IntentFilter filter = new IntentFilter();
        filter.addAction(Intent.ACTION_SCREEN_OFF);
        filter.addAction(Intent.ACTION_SCREEN_ON);
        registerReceiver(screenReceiver, filter);
        Log.i(TAG, "service onCreate");
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        ensureLaunchAssistOverlay();
        Log.i(TAG, "service onStartCommand");
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        unregisterReceiver(screenReceiver);
        removeLaunchAssistOverlay();
        Log.i(TAG, "service onDestroy");
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
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
            .setContentText("Armed for trusted Android keyguard dismissal")
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build();
    }
}
