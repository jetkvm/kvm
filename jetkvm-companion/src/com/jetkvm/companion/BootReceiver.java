package com.jetkvm.companion;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.util.Log;

public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        String action = intent.getAction();
        if (!Intent.ACTION_BOOT_COMPLETED.equals(action)
                && !Intent.ACTION_LOCKED_BOOT_COMPLETED.equals(action)) {
            return;
        }

        Context storageContext = android.os.Build.VERSION.SDK_INT >= 24
            ? context.createDeviceProtectedStorageContext()
            : context;
        SharedPreferences prefs = storageContext.getSharedPreferences(CompanionService.PREFS, Context.MODE_PRIVATE);
        if (!prefs.getBoolean(CompanionService.KEY_LAUNCH_ON_BOOT, false)) {
            Log.i(CompanionService.TAG, action + "; launch on boot disabled");
            return;
        }

        Log.i(CompanionService.TAG, action + "; starting companion service");
        Intent service = new Intent(context, CompanionService.class);
        context.startForegroundService(service);
    }
}
