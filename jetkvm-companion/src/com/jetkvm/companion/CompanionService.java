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
import android.os.IBinder;
import android.util.Log;

public class CompanionService extends Service {
    static final String TAG = "JetKVMCompanion";
    static final String ACTION_SCREEN_OFF = "com.jetkvm.companion.SCREEN_OFF";
    static final String ACTION_SCREEN_ON = "com.jetkvm.companion.SCREEN_ON";
    static final String PREFS = "jetkvm_companion";
    static final String KEY_LAUNCH_ON_BOOT = "launch_on_boot";

    private static final String CHANNEL_ID = "jetkvm-companion";
    private static final int NOTIFICATION_ID = 1001;

    private final BroadcastReceiver screenReceiver = new BroadcastReceiver() {
        @Override
        public void onReceive(Context context, Intent intent) {
            String action = intent.getAction();
            Log.i(TAG, "screen receiver action=" + action);
            if (Intent.ACTION_SCREEN_OFF.equals(action)) {
                launchDismissActivity(ACTION_SCREEN_OFF);
            } else if (Intent.ACTION_SCREEN_ON.equals(action)) {
                launchDismissActivity(ACTION_SCREEN_ON);
            }
        }
    };

    @Override
    public void onCreate() {
        super.onCreate();
        createChannel();
        startForeground(NOTIFICATION_ID, buildNotification());

        IntentFilter filter = new IntentFilter();
        filter.addAction(Intent.ACTION_SCREEN_OFF);
        filter.addAction(Intent.ACTION_SCREEN_ON);
        registerReceiver(screenReceiver, filter);
        Log.i(TAG, "service onCreate");
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        Log.i(TAG, "service onStartCommand");
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        unregisterReceiver(screenReceiver);
        Log.i(TAG, "service onDestroy");
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    private void launchDismissActivity(String action) {
        Intent activity = new Intent(this, MainActivity.class);
        activity.setAction(action);
        activity.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        Log.i(TAG, "starting dismiss activity action=" + action);
        startActivity(activity);
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
