package com.partyplayer.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.os.Build;
import android.os.IBinder;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;

/**
 * 前台服务：持有 Go 服务器子进程，保证锁屏 / 切后台时播放不中断。
 * 服务器二进制以 libpartyplayer.so 形式打包在 jniLibs 中，
 * 运行时从 nativeLibraryDir exec（Android 10+ 唯一允许执行的位置）。
 */
public class PlayerService extends Service {

    private static final String CHANNEL = "playback";
    private static final String ACTION_STOP = "com.partyplayer.app.STOP";
    private static final String TAG = "PartyPlayer";
    private Process proc;

    @Override
    public void onCreate() {
        super.onCreate();
        NotificationManager nm = getSystemService(NotificationManager.class);
        Notification.Builder b;
        if (Build.VERSION.SDK_INT >= 26) {
            nm.createNotificationChannel(new NotificationChannel(
                    CHANNEL, "播放服务", NotificationManager.IMPORTANCE_LOW));
            b = new Notification.Builder(this, CHANNEL);
        } else {
            b = new Notification.Builder(this);
        }
        Intent open = new Intent(this, MainActivity.class);
        b.setContentTitle("舞会音乐播放器")
                .setContentText("正在运行，点击返回页面")
                .setSmallIcon(R.drawable.ic_stat)
                .setContentIntent(PendingIntent.getActivity(
                        this, 0, open, PendingIntent.FLAG_IMMUTABLE))
                .addAction(new Notification.Action.Builder(
                        null, "停止",
                        PendingIntent.getService(this, 1,
                                new Intent(this, PlayerService.class).setAction(ACTION_STOP),
                                PendingIntent.FLAG_IMMUTABLE))
                        .build());
        startForeground(1, b.build());
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            stopSelf();
            return START_NOT_STICKY;
        }
        final int port = intent != null ? intent.getIntExtra("port", 8765) : 8765;
        // 日志泵放后台线程，避免阻塞 onStartCommand
        new Thread(() -> startServer(port), "pp-server").start();
        return START_NOT_STICKY;
    }

    private void startServer(int port) {
        if (proc != null) {
            return;
        }
        try {
            File bin = new File(getApplicationInfo().nativeLibraryDir, "libpartyplayer.so");
            File data = new File(getFilesDir(), "data");
            data.mkdirs();
            ProcessBuilder pb = new ProcessBuilder(
                    bin.getAbsolutePath(),
                    "-data", data.getAbsolutePath(),
                    "-port", String.valueOf(port),
                    "-listen", "0.0.0.0",
                    "-no-browser");
            pb.redirectErrorStream(true);
            proc = pb.start();
            BufferedReader r = new BufferedReader(
                    new InputStreamReader(proc.getInputStream(), StandardCharsets.UTF_8));
            String line;
            while ((line = r.readLine()) != null) {
                android.util.Log.i(TAG, line);
            }
        } catch (Exception e) {
            android.util.Log.e(TAG, "服务器进程异常退出", e);
        }
    }

    @Override
    public void onDestroy() {
        if (proc != null) {
            proc.destroy();
            proc = null;
        }
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
