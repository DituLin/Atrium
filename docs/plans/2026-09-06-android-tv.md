# Android TV and Core stability implementation plan

Goal: deliver a standalone Android TV APK, install and verify it on OnePlus 6T, and address observed Core instability.

Architecture: a native landscape Activity with TV/mobile launcher entries owns connection setup, fullscreen, lifecycle, connection failure recovery, and remote-key dispatch. A restricted same-origin WebView uses the existing paired React client. Build-time local CA provisioning uses Android network security configuration (never SSL-error bypass); household URL and public CA remain local build inputs, not repository data. Native setup permits changing the HTTPS origin. Cookie storage persists pairing; app backup is disabled.

Stack: Android SDK 35, Java 17, Gradle/AGP available locally, platform WebView; Go/SQLite Core and React frontend.

1. Inspect runtime logs, SQLite concurrency and worker completion paths. Reproduce root causes with targeted Go tests before changing implementation. Verify deployed binary provenance; preserve database/configuration and rollback binary before deployment.
2. Add android-tv/ Gradle project, manifest, TV banner/icon, native connection screen and same-origin policy. Unit-test origin validation and error boundaries. Build using local SDK/JDK.
3. Add immersive landscape WebView, app-only CA trust, persistent cookies, no file/content access or mixed content, fail-closed TLS handling, native offline retry, renderer recovery and lifecycle callbacks. Map D-pad/Enter/Back to current Web client. Include a focusable settings/exit path without trapping users.
4. Verify frontend layout on the actual WebView and adjust responsive constraints if needed; retain large-display layout.
5. Build, provision local public CA, install APK on OnePlus 6T, pair separately, inspect actual rendered UI. Test dashboard/photos, navigate/show/refresh, D-pad/back, process restart pairing, foreground recovery and temporary network interruption. Restore working dashboard.
6. Run appropriate Go, web, Android unit/build checks. Record measured results, limitations and install/build instructions; retain APK artifact. Long-run reliability remains a separately measured operational gate, never inferred from smoke tests.
