# Keep Kotlin metadata for reflection-based libraries
-keep class kotlin.Metadata { *; }

# Keep the Go launcher service
-keep class com.glmproxy.app.** { *; }

# Keep WebView JS bridge (if added later)
-keepclassmembers class * {
    @android.webkit.JavascriptInterface <methods>;
}
