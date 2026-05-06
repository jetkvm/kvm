package com.jetkvm.companion;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.util.Log;

public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        if (!Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) return;

        SharedPreferences prefs = context.getSharedPreferences(CompanionService.PREFS, Context.MODE_PRIVATE);
        if (!prefs.getBoolean(CompanionService.KEY_LAUNCH_ON_BOOT, false)) {
            Log.i(CompanionService.TAG, "boot completed; launch on boot disabled");
            return;
        }

        Log.i(CompanionService.TAG, "boot completed; starting companion service");
        Intent service = new Intent(context, CompanionService.class);
        context.startForegroundService(service);
    }
}
