package io.atrium.tv;

import android.app.Activity;
import android.graphics.Color;
import android.view.Gravity;
import android.view.KeyEvent;
import android.webkit.CookieManager;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;
import androidx.annotation.OptIn;
import androidx.media3.common.MediaItem;
import androidx.media3.common.PlaybackException;
import androidx.media3.common.Player;
import androidx.media3.common.util.UnstableApi;
import androidx.media3.datasource.DefaultHttpDataSource;
import androidx.media3.exoplayer.ExoPlayer;
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory;
import androidx.media3.ui.PlayerView;
import java.util.HashMap;
import java.util.Map;

/**
 * Full-screen native playback over the WebView. The TV's WebView (Chrome 66)
 * cannot decode HEVC, but the platform decoders can; ExoPlayer uses them.
 *
 * Remote: OK play/pause (replay at the end), Left/Right seek 10 s, Up/Down show
 * the controls, Back closes. The page learns the outcome through `Listener`.
 */
@OptIn(markerClass = UnstableApi.class)
final class NativeVideoPlayer {
    interface Listener { void onClosed(long positionMs, long durationMs, boolean ended, String error); }

    private static final long SEEK_MS = 10_000;
    private final Activity activity;
    private final FrameLayout root;
    private final Listener listener;
    private ExoPlayer player;
    private FrameLayout layer;
    private PlayerView view;
    private TextView status;
    private boolean ended;
    private String error;

    NativeVideoPlayer(Activity activity, FrameLayout root, Listener listener) {
        this.activity = activity;
        this.root = root;
        this.listener = listener;
    }

    boolean isOpen() { return player != null; }

    /** ticket may be null; hosts then rely on the screen cookie. */
    void open(String url, String origin, String ticket, String title, String subtitle) {
        if (isOpen()) close();
        ended = false;
        error = null;
        Map<String, String> headers = new HashMap<>();
        String cookie = CookieManager.getInstance().getCookie(origin);
        if (cookie != null) headers.put("Cookie", cookie);
        // WebView 66 withholds the SameSite screen cookie from getCookie(); the
        // page obtains a ticket for this one video and hands it over instead.
        if (ticket != null) headers.put(VideoRequest.TICKET_HEADER, ticket);
        // Cookie credentials must prove their origin to Core (design §6.8).
        headers.put("Origin", origin);
        headers.put("Referer", origin + "/");
        DefaultHttpDataSource.Factory http = new DefaultHttpDataSource.Factory()
            .setDefaultRequestProperties(headers)
            .setAllowCrossProtocolRedirects(false)
            .setConnectTimeoutMs(15_000)
            .setReadTimeoutMs(30_000);
        player = new ExoPlayer.Builder(activity)
            .setMediaSourceFactory(new DefaultMediaSourceFactory(http))
            .setSeekBackIncrementMs(SEEK_MS)
            .setSeekForwardIncrementMs(SEEK_MS)
            .build();
        player.addListener(new Player.Listener() {
            @Override public void onPlaybackStateChanged(int state) {
                if (state == Player.STATE_ENDED) { ended = true; showStatus("已放映完毕 · OK 重新播放 · 返回退出"); }
                else if (state == Player.STATE_READY) hideStatus();
                else if (state == Player.STATE_BUFFERING) showStatus("正在读取视频…");
            }
            @Override public void onPlayerError(PlaybackException e) {
                error = e.getErrorCodeName();
                boolean decoder = e.errorCode >= PlaybackException.ERROR_CODE_DECODER_INIT_FAILED
                    && e.errorCode <= PlaybackException.ERROR_CODE_DECODING_FORMAT_UNSUPPORTED;
                showStatus(decoder ? "此设备无法解码这段视频 · 返回退出" : "视频读取失败 · OK 重试 · 返回退出");
            }
        });

        layer = new FrameLayout(activity);
        layer.setBackgroundColor(Color.BLACK);
        view = new PlayerView(activity);
        view.setPlayer(player);
        view.setUseController(true);
        view.setControllerShowTimeoutMs(3_000);
        view.setShowNextButton(false);
        view.setShowPreviousButton(false);
        view.setKeepScreenOn(true);
        layer.addView(view, new FrameLayout.LayoutParams(-1, -1));

        LinearLayout heading = new LinearLayout(activity);
        heading.setOrientation(LinearLayout.VERTICAL);
        heading.setPadding(dp(32), dp(20), dp(32), dp(20));
        heading.addView(text(VideoRequest.label(title, "家庭影像"), 24, Color.rgb(238, 233, 222)));
        String sub = VideoRequest.label(subtitle, "");
        if (!sub.isEmpty()) heading.addView(text(sub, 14, Color.rgb(201, 208, 200)));
        layer.addView(heading, new FrameLayout.LayoutParams(-2, -2, Gravity.TOP | Gravity.START));

        status = text("", 20, Color.rgb(238, 233, 222));
        status.setBackgroundColor(Color.argb(200, 18, 23, 20));
        status.setPadding(dp(20), dp(10), dp(20), dp(10));
        status.setVisibility(TextView.GONE);
        layer.addView(status, new FrameLayout.LayoutParams(-2, -2, Gravity.CENTER));

        root.addView(layer, new FrameLayout.LayoutParams(-1, -1));
        player.setMediaItem(MediaItem.fromUri(url));
        player.prepare();
        player.setPlayWhenReady(true);
        showStatus("正在读取视频…");
    }

    /** Handles every key while open; nothing reaches the page underneath. */
    boolean dispatchKey(KeyEvent e) {
        if (!isOpen()) return false;
        int code = e.getKeyCode();
        if (code == KeyEvent.KEYCODE_BACK || code == KeyEvent.KEYCODE_ESCAPE) {
            if (e.getAction() == KeyEvent.ACTION_UP && !e.isCanceled()) close();
            return true;
        }
        if (e.getAction() != KeyEvent.ACTION_DOWN) return true;
        switch (code) {
            case KeyEvent.KEYCODE_DPAD_CENTER:
            case KeyEvent.KEYCODE_ENTER:
            case KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE:
                if (e.getRepeatCount() == 0) toggle();
                break;
            case KeyEvent.KEYCODE_DPAD_LEFT: player.seekBack(); break;
            case KeyEvent.KEYCODE_DPAD_RIGHT: player.seekForward(); break;
            case KeyEvent.KEYCODE_MEDIA_PLAY: player.play(); break;
            case KeyEvent.KEYCODE_MEDIA_PAUSE: player.pause(); break;
            default: break;
        }
        view.showController();
        return true;
    }

    private void toggle() {
        if (error != null) { error = null; player.prepare(); player.play(); return; }
        if (player.getPlaybackState() == Player.STATE_ENDED) { ended = false; player.seekTo(0); player.play(); return; }
        if (player.isPlaying()) player.pause(); else player.play();
    }

    /** Leaving the foreground pauses; playback never continues out of sight. */
    void pause() { if (player != null) player.pause(); }

    void close() {
        if (player == null) return;
        long position = Math.max(0, player.getCurrentPosition());
        long duration = Math.max(0, player.getDuration());
        boolean finished = ended;
        String failure = error;
        player.release();
        player = null;
        root.removeView(layer);
        layer = null;
        view = null;
        status = null;
        listener.onClosed(position, duration, finished, failure);
    }

    private void showStatus(String message) { if (status != null) { status.setText(message); status.setVisibility(TextView.VISIBLE); } }
    private void hideStatus() { if (status != null) status.setVisibility(TextView.GONE); }
    private TextView text(String value, int sp, int color) {
        TextView t = new TextView(activity);
        t.setText(value);
        t.setTextSize(sp);
        t.setTextColor(color);
        return t;
    }
    private int dp(int n) { return Math.round(n * activity.getResources().getDisplayMetrics().density); }
}
