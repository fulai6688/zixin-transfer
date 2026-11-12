package cn.zixin.transfer;

import android.app.Activity;
import android.app.AlertDialog;
import android.app.DownloadManager;
import android.content.Context;
import android.content.DialogInterface;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.Uri;
import android.os.Bundle;
import android.os.Environment;
import android.text.InputType;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.CookieManager;
import android.webkit.DownloadListener;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.Toast;

/**
 * 子欣互传 Android 客户端。
 *
 * 本机不需要跑节点：这个壳把浏览器里的同一套自研 UI 打开，指向你在电脑上运行的
 * 子欣互传（例如 http://192.168.1.20:8080）。它补上了浏览器做不到的三件事：
 *   · 记住服务器地址（长按页面可修改）
 *   · 聊天室里点文件能真的下载到本机「下载」目录
 *   · 发文件时能调起系统文件选择器
 */
public class MainActivity extends Activity {

    private static final String PREFS = "zixin";
    private static final String KEY_BASE = "base";
    private static final int REQ_FILE = 1001;

    private WebView web;
    private ValueCallback<Uri[]> fileCallback;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        FrameLayout root = new FrameLayout(this);
        web = new WebView(this);
        root.addView(web, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        setContentView(root);

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setDatabaseEnabled(true);
        s.setAllowFileAccess(false);
        s.setMediaPlaybackRequiresUserGesture(false);
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);
        s.setCacheMode(WebSettings.LOAD_DEFAULT);
        s.setUseWideViewPort(true);
        s.setLoadWithOverviewMode(true);

        CookieManager.getInstance().setAcceptCookie(true);
        web.setWebViewClient(new WebViewClient());
        web.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> cb,
                                             FileChooserParams params) {
                if (fileCallback != null) {
                    fileCallback.onReceiveValue(null);
                }
                fileCallback = cb;
                Intent i = new Intent(Intent.ACTION_GET_CONTENT);
                i.addCategory(Intent.CATEGORY_OPENABLE);
                i.setType("*/*");
                try {
                    startActivityForResult(Intent.createChooser(i, "选择要发送的文件"), REQ_FILE);
                    return true;
                } catch (Exception e) {
                    fileCallback = null;
                    return false;
                }
            }
        });

        // 聊天室里的文件是普通链接，交给系统下载器，落到「下载」目录
        web.setDownloadListener(new DownloadListener() {
            @Override
            public void onDownloadStart(String url, String userAgent, String contentDisposition,
                                        String mimeType, long contentLength) {
                try {
                    DownloadManager.Request req = new DownloadManager.Request(Uri.parse(url));
                    String name = android.webkit.URLUtil.guessFileName(url, contentDisposition, mimeType);
                    req.setMimeType(mimeType);
                    req.addRequestHeader("User-Agent", userAgent);
                    req.setNotificationVisibility(
                            DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED);
                    req.setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name);
                    DownloadManager dm = (DownloadManager) getSystemService(Context.DOWNLOAD_SERVICE);
                    if (dm != null) {
                        dm.enqueue(req);
                        Toast.makeText(MainActivity.this, "开始下载 " + name, Toast.LENGTH_SHORT).show();
                    }
                } catch (Exception e) {
                    // 兜底：交给系统浏览器打开
                    try {
                        startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url)));
                    } catch (Exception ignored) {
                    }
                }
            }
        });

        // 长按页面 = 修改服务器地址（壳里没有菜单键）
        web.setOnLongClickListener(new View.OnLongClickListener() {
            @Override
            public boolean onLongClick(View v) {
                askBase(null);
                return true;
            }
        });

        // 支持外部预填地址（adb / 快捷方式 / 二维码跳转都可用）：
        //   adb shell am start -n cn.zixin.transfer/.MainActivity -e base 192.168.1.20:8080
        String fromIntent = getIntent() != null ? getIntent().getStringExtra("base") : null;
        if (fromIntent != null && !fromIntent.trim().isEmpty()) {
            prefs().edit().putString(KEY_BASE, fromIntent.trim()).apply();
            load(fromIntent.trim());
            return;
        }

        String base = prefs().getString(KEY_BASE, "");
        if (base == null || base.isEmpty()) {
            askBase(null);
        } else {
            load(base);
        }
    }

    private SharedPreferences prefs() {
        return getSharedPreferences(PREFS, MODE_PRIVATE);
    }

    private void load(String base) {
        String url = base;
        if (!url.startsWith("http://") && !url.startsWith("https://")) {
            url = "http://" + url;
        }
        if (!url.endsWith("/")) {
            url = url + "/";
        }
        web.loadUrl(url);
    }

    /** 弹出地址输入框。hint 为 null 时显示当前值。 */
    private void askBase(String preset) {
        final EditText input = new EditText(this);
        input.setInputType(InputType.TYPE_TEXT_VARIATION_URI);
        input.setSingleLine(true);
        String cur = preset != null ? preset : prefs().getString(KEY_BASE, "");
        if (cur != null && !cur.isEmpty()) {
            input.setText(cur);
            input.setSelection(cur.length());
        }
        input.setHint("192.168.1.20:8080");

        new AlertDialog.Builder(this)
                .setTitle("子欣互传")
                .setMessage("填写电脑上运行的服务地址（同一 WiFi 下）")
                .setView(input)
                .setCancelable(false)
                .setPositiveButton("连接", new DialogInterface.OnClickListener() {
                    @Override
                    public void onClick(DialogInterface d, int w) {
                        String v = input.getText().toString().trim();
                        if (v.isEmpty()) {
                            Toast.makeText(MainActivity.this, "地址不能为空", Toast.LENGTH_SHORT).show();
                            askBase(null);
                            return;
                        }
                        prefs().edit().putString(KEY_BASE, v).apply();
                        load(v);
                    }
                })
                .setNegativeButton("退出", new DialogInterface.OnClickListener() {
                    @Override
                    public void onClick(DialogInterface d, int w) {
                        finish();
                    }
                })
                .show();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode == REQ_FILE) {
            if (fileCallback != null) {
                Uri[] result = null;
                if (resultCode == RESULT_OK && data != null && data.getData() != null) {
                    result = new Uri[]{data.getData()};
                }
                fileCallback.onReceiveValue(result);
                fileCallback = null;
            }
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    @Override
    public void onBackPressed() {
        if (web != null && web.canGoBack()) {
            web.goBack();
        } else {
            super.onBackPressed();
        }
    }
}
