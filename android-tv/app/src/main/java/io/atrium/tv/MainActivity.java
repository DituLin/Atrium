package io.atrium.tv;

import android.app.Activity;
import android.app.AlertDialog;
import android.graphics.Color;
import android.net.http.SslError;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.WindowManager;
import android.webkit.CookieManager;
import android.webkit.RenderProcessGoneDetail;
import android.webkit.SslErrorHandler;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;
import java.io.ByteArrayInputStream;

/** TV host: no Chrome dependency, no JS-to-native bridge, no SSL bypass. */
public final class MainActivity extends Activity {
    private final Handler handler = new Handler(Looper.getMainLooper());
    private FrameLayout root;
    private WebView web;
    private View overlay;
    private String origin = "";
    private boolean resumed, failed, tlsFailed;
    private int retryDelay = 1000;
    private long lastBack;
    private final Runnable retry = () -> { if (resumed && failed && !tlsFailed && web != null) web.reload(); };

    // The SPA cannot reconnect until its startup bundle has run. An HTML 200
    // followed by a failed module download needs native recovery as well.
    private final Runnable checkBoot = () -> {
        WebView current=web;
        if(!resumed || current==null || failed)return;
        current.evaluateJavascript("!!document.getElementById('root') && !document.getElementById('boot')", result -> {
            if(current==web && resumed && !failed && !"true".equals(result))
                connectionError(false,"页面资源未加载完成，正在重新连接。");
        });
    };

    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        root = new FrameLayout(this);
        root.setBackgroundColor(Color.rgb(11,13,16));
        setContentView(root);
        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG);
        origin = getPreferences(MODE_PRIVATE).getString("origin", "");
        if (origin.isEmpty()) setup(); else connect();
    }
    private int dp(int n) { return Math.round(n * getResources().getDisplayMetrics().density); }
    private void immersive() {
        getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY |
            View.SYSTEM_UI_FLAG_FULLSCREEN | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION |
            View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION |
            View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
    }
    @Override public void onWindowFocusChanged(boolean focused) { super.onWindowFocusChanged(focused); if(focused) immersive(); }
    private LinearLayout panel(String title, String detail) {
        clearOverlay();
        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL); box.setGravity(Gravity.CENTER_VERTICAL);
        box.setPadding(dp(36),dp(12),dp(36),dp(12)); box.setBackgroundColor(Color.rgb(11,13,16));
        TextView heading = new TextView(this); heading.setText(title); heading.setTextSize(28); heading.setTextColor(Color.WHITE);
        box.addView(heading);
        TextView body = new TextView(this); body.setText(detail); body.setTextSize(16); body.setTextColor(Color.LTGRAY);
        box.addView(body);
        root.addView(box,new FrameLayout.LayoutParams(-1,-1)); overlay=box;
        return box;
    }
    private Button button(LinearLayout parent,String label,Runnable action) {
        Button b = new Button(this); b.setText(label); b.setAllCaps(false);
        b.setOnClickListener(v -> action.run()); parent.addView(b,new LinearLayout.LayoutParams(-1,dp(48)));
        return b;
    }
    private void clearOverlay() { if(overlay!=null) {root.removeView(overlay);overlay=null;} }
    private void setup() {
        handler.removeCallbacks(retry); destroyWeb();
        LinearLayout box=panel("Atrium · 家庭屏幕", "输入 Mac mini 的 HTTPS 地址。使用随本应用配置的家庭 CA，无需安装浏览器。");
        EditText address=new EditText(this); address.setSingleLine(true); address.setTextSize(18);
        address.setInputType(android.text.InputType.TYPE_CLASS_TEXT | android.text.InputType.TYPE_TEXT_VARIATION_URI);
        address.setHint("https://macmini.local:8443");address.setText(origin);box.addView(address,new LinearLayout.LayoutParams(-1,dp(48)));
        button(box,"连接家庭中枢",()->{
            try {
                String next=OriginPolicy.normalize(address.getText().toString());
                origin=next;getPreferences(MODE_PRIVATE).edit().putString("origin",origin).apply();connect();
            } catch(IllegalArgumentException e) {address.setError("请输入有效的 HTTPS 地址（不含路径）");address.requestFocus();}
        });
        button(box,"退出",this::finish);
        address.requestFocus();
    }
    @SuppressWarnings("SetJavaScriptEnabled")
    private void connect() {
        handler.removeCallbacks(retry); destroyWeb();clearOverlay();
        failed=false;tlsFailed=false;retryDelay=1000;
        web=new WebView(this);web.setBackgroundColor(Color.rgb(11,13,16));
        WebSettings s=web.getSettings();s.setJavaScriptEnabled(true);s.setDomStorageEnabled(true);
        s.setAllowFileAccess(false);s.setAllowContentAccess(false);s.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        s.setSupportMultipleWindows(false);s.setJavaScriptCanOpenWindowsAutomatically(false);
        s.setTextZoom(100);s.setSupportZoom(false);s.setBuiltInZoomControls(false);s.setDisplayZoomControls(false);
        s.setUseWideViewPort(true);s.setLoadWithOverviewMode(true);
        CookieManager.getInstance().setAcceptCookie(true);CookieManager.getInstance().setAcceptThirdPartyCookies(web,false);
        web.setWebChromeClient(new WebChromeClient());
        web.setWebViewClient(new WebViewClient(){
            @Override public boolean shouldOverrideUrlLoading(WebView v,WebResourceRequest r) {return !OriginPolicy.allows(origin,r.getUrl().toString());}
            @Override public WebResourceResponse shouldInterceptRequest(WebView v,WebResourceRequest r) {
                if(OriginPolicy.allows(origin,r.getUrl().toString())) return null;
                return new WebResourceResponse("text/plain","UTF-8",403,"Blocked",java.util.Collections.emptyMap(),new ByteArrayInputStream(new byte[0]));
            }
            @Override public void onPageStarted(WebView v,String url,android.graphics.Bitmap icon) {failed=false;handler.removeCallbacks(checkBoot);handler.postDelayed(checkBoot,10000);}
            @Override public void onPageFinished(WebView v,String url) {
                if(!failed && OriginPolicy.allows(origin,url)) {clearOverlay();retryDelay=1000;CookieManager.getInstance().flush();v.requestFocus();}
            }
            @Override public void onReceivedError(WebView v,WebResourceRequest r,WebResourceError e) {
                if(r.isForMainFrame()) connectionError(false,"无法连接家庭中枢。请检查 Mac mini 和局域网，应用会自动重试。");
            }
            @Override public void onReceivedHttpError(WebView v,WebResourceRequest r,WebResourceResponse response) {
                if(r.isForMainFrame() && response.getStatusCode()>=400) connectionError(false,"家庭中枢返回错误，正在等待恢复。");
            }
            @Override public void onReceivedSslError(WebView v,SslErrorHandler h,SslError error) {
                h.cancel(); connectionError(true,"证书验证失败。请检查设备时间、服务地址，以及安装包所配置的家庭 CA。不会忽略证书错误。");
            }
            @Override public boolean onRenderProcessGone(WebView v,RenderProcessGoneDetail detail) {
                handler.post(()->{destroyWeb();LinearLayout box=panel("展示已中断","WebView 渲染进程已退出。");button(box,"恢复展示",MainActivity.this::connect).requestFocus();});return true;
            }
        });
        root.addView(web,new FrameLayout.LayoutParams(-1,-1));web.requestFocus();web.loadUrl(origin+"/");immersive();
    }
    private void connectionError(boolean tls,String detail) {
        failed=true;tlsFailed=tlsFailed||tls;
        handler.removeCallbacks(retry);
        LinearLayout box=panel(tlsFailed?"连接需要处理":"正在重新连接",detail);
        button(box,"重试",()->{tlsFailed=false;web.reload();}).requestFocus();
        button(box,"连接设置",this::setup);
        if(!tlsFailed && resumed){handler.postDelayed(retry,retryDelay);retryDelay=Math.min(30000,retryDelay*2);}
    }
    private void menu() {
        new AlertDialog.Builder(this).setTitle("Atrium")
            .setItems(new String[]{"继续展示","连接设置","退出应用"},(d,index)->{if(index==1)setup();if(index==2)finish();})
            .show();
    }
    private void sendKey(String key,int code) {
        if(web!=null)web.evaluateJavascript("(()=>{const t=document.activeElement||document.body;const unhandled=t.dispatchEvent(new KeyboardEvent('keydown',{key:'"+key+"',keyCode:"+code+",which:"+code+",bubbles:true,cancelable:true}));if(unhandled&&'"+key+"'==='Enter'&&t.matches('button,a,[role=button]'))t.click();})()",null);
    }
    @Override public boolean dispatchKeyEvent(KeyEvent e) {
        if(e.getKeyCode()==KeyEvent.KEYCODE_MENU) {if(e.getAction()==KeyEvent.ACTION_UP)menu();return true;}
        if(web!=null && overlay==null) {
            String key=null;int code=0;
            switch(e.getKeyCode()) {
                case KeyEvent.KEYCODE_DPAD_LEFT:key="ArrowLeft";code=37;break;
                case KeyEvent.KEYCODE_DPAD_UP:key="ArrowUp";code=38;break;
                case KeyEvent.KEYCODE_DPAD_RIGHT:key="ArrowRight";code=39;break;
                case KeyEvent.KEYCODE_DPAD_DOWN:key="ArrowDown";code=40;break;
                case KeyEvent.KEYCODE_DPAD_CENTER:case KeyEvent.KEYCODE_ENTER:key="Enter";code=13;break;
                case KeyEvent.KEYCODE_BACK:case KeyEvent.KEYCODE_ESCAPE:
                    if(e.getAction()==KeyEvent.ACTION_UP) {
                        long now=android.os.SystemClock.elapsedRealtime();
                        if(now-lastBack<700){lastBack=0;menu();}else{lastBack=now;sendKey("Escape",27);}
                    }return true;
            }
            if(key!=null){if(e.getAction()==KeyEvent.ACTION_DOWN)sendKey(key,code);return true;}
        }
        return super.dispatchKeyEvent(e);
    }
    @Override public void onBackPressed(){if(web==null)finish();else menu();}
    @Override protected void onResume(){super.onResume();resumed=true;immersive();handler.removeCallbacks(checkBoot);handler.postDelayed(checkBoot,10000);if(web!=null){web.onResume();if(failed&&!tlsFailed)handler.post(retry);}}
    @Override protected void onPause(){resumed=false;handler.removeCallbacks(retry);handler.removeCallbacks(checkBoot);if(web!=null)web.onPause();CookieManager.getInstance().flush();super.onPause();}
    private void destroyWeb(){handler.removeCallbacks(checkBoot);if(web!=null){web.stopLoading();root.removeView(web);web.destroy();web=null;}}
    @Override protected void onDestroy(){handler.removeCallbacksAndMessages(null);destroyWeb();super.onDestroy();}
}
