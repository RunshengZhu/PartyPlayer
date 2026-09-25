package com.partyplayer.app;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.provider.DocumentsContract;
import android.provider.Settings;
import android.webkit.JavascriptInterface;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import org.json.JSONObject;

import java.io.File;
import java.net.HttpURLConnection;
import java.net.ServerSocket;
import java.net.URL;

public class MainActivity extends Activity {

    private static final int REQ_FILE = 42;      // 歌单导入
    private static final int REQ_TREE = 43;      // 曲库目录（SAF）
    private WebView web;
    private ValueCallback<Uri[]> fileCallback;
    private int port;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        requestNotificationPermission();

        port = freePort();
        Intent svc = new Intent(this, PlayerService.class);
        svc.putExtra("port", port);
        startForegroundService(svc);

        web = new WebView(this);
        setContentView(web);
        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setMediaPlaybackRequiresUserGesture(false);
        web.setWebViewClient(new WebViewClient());
        // 支持网页里的文件选择（歌单导入）
        web.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onShowFileChooser(WebView wv, ValueCallback<Uri[]> cb,
                                             FileChooserParams params) {
                if (fileCallback != null) {
                    fileCallback.onReceiveValue(null);
                }
                fileCallback = cb;
                try {
                    startActivityForResult(params.createIntent(), REQ_FILE);
                    return true;
                } catch (Exception e) {
                    fileCallback = null;
                    return false;
                }
            }
        });
        // 供网页判断运行环境并拉起系统目录选择器
        web.addJavascriptInterface(new NativeBridge(), "NativeShell");

        final String url = "http://127.0.0.1:" + port;
        final Activity self = this;
        new Thread(() -> {
            // 等待 Go 服务器就绪（最多约 10 秒）
            for (int i = 0; i < 100; i++) {
                if (httpOK(url + "/api/state")) break;
                try { Thread.sleep(100); } catch (InterruptedException ignored) { return; }
            }
            self.runOnUiThread(() -> web.loadUrl(url));
        }).start();
    }

    /** 暴露给网页的原生能力桥。 */
    private class NativeBridge {

        @JavascriptInterface
        public boolean isAndroid() {
            return true;
        }

        /** 拉起系统文件管理器的目录选择器；结果经 __onNativeFolder 回调网页。 */
        @JavascriptInterface
        public void pickFolder() {
            runOnUiThread(() -> {
                if (!ensureStorageAccess()) {
                    sendPickResult(null, "storage");
                    return;
                }
                try {
                    startActivityForResult(
                            new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE), REQ_TREE);
                } catch (Exception e) {
                    sendPickResult(null, "unavailable");
                }
            });
        }
    }

    /** 确保可以按真实路径读取共享存储；不满足时引导用户授权，返回 false。 */
    private boolean ensureStorageAccess() {
        if (Build.VERSION.SDK_INT >= 30) {
            if (Environment.isExternalStorageManager()) {
                return true;
            }
            try {
                startActivity(new Intent(
                        Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION,
                        Uri.parse("package:" + getPackageName())));
            } catch (Exception e) {
                startActivity(new Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION));
            }
            return false;
        }
        if (checkSelfPermission(Manifest.permission.READ_EXTERNAL_STORAGE)
                == PackageManager.PERMISSION_GRANTED) {
            return true;
        }
        requestPermissions(new String[]{Manifest.permission.READ_EXTERNAL_STORAGE}, 2);
        return false;
    }

    /** 把 SAF 目录树 URI 换算成真实路径（仅支持主存储：primary:xxx）。 */
    private String treeUriToPath(Uri uri) {
        try {
            String docId = DocumentsContract.getTreeDocumentId(uri);
            String[] parts = docId.split(":", 2);
            if (parts.length != 2 || !"primary".equals(parts[0])) {
                return null;
            }
            return new File(Environment.getExternalStorageDirectory(), parts[1])
                    .getAbsolutePath();
        } catch (Exception e) {
            return null;
        }
    }

    private void sendPickResult(String path, String reason) {
        if (web == null) {
            return;
        }
        try {
            JSONObject json = new JSONObject();
            json.put("path", path == null ? JSONObject.NULL : path);
            json.put("reason", reason == null ? JSONObject.NULL : reason);
            final String js = "window.__onNativeFolder && window.__onNativeFolder(" + json + ")";
            runOnUiThread(() -> web.evaluateJavascript(js, null));
        } catch (Exception ignored) {
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode == REQ_FILE) {
            if (fileCallback != null) {
                fileCallback.onReceiveValue(
                        WebChromeClient.FileChooserParams.parseResult(resultCode, data));
                fileCallback = null;
            }
            return;
        }
        if (requestCode == REQ_TREE) {
            Uri uri = (data != null && data.getData() != null) ? data.getData() : null;
            String path = uri == null ? null : treeUriToPath(uri);
            sendPickResult(path, path == null ? "cancel" : null);
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    private int freePort() {
        try (ServerSocket ss = new ServerSocket(0)) {
            return ss.getLocalPort();
        } catch (Exception e) {
            return 8765;
        }
    }

    private boolean httpOK(String u) {
        try {
            HttpURLConnection c = (HttpURLConnection) new URL(u).openConnection();
            c.setConnectTimeout(500);
            c.setReadTimeout(500);
            int code = c.getResponseCode();
            c.disconnect();
            return code == 200;
        } catch (Exception e) {
            return false;
        }
    }

    private void requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= 33 &&
                checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                        != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 1);
        }
    }

    @Override
    public void onBackPressed() {
        // 返回键退到后台而不是销毁 WebView，保持播放
        if (web != null && web.canGoBack()) {
            web.goBack();
        } else {
            moveTaskToBack(true);
        }
    }

    @Override
    protected void onDestroy() {
        if (web != null) {
            web.destroy();
            web = null;
        }
        super.onDestroy();
    }
}
