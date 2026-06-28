import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "com.glmproxy.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.glmproxy.app"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "1.0.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // Only ship the arm64-v8a ABI — the embedded Go binary is built
        // exclusively for arm64. Including other ABIs would crash on x86_64
        // emulators etc. For testing on x86_64 emulators, build a second
        // binary at jniLibs/x86_64/libglmproxy.so.
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    // Plan 019: test infrastructure. Robolectric lets us run tests that
    // depend on Android framework classes (Context, NotificationManager,
    // etc.) on the JVM without an emulator. ReturnDefaultValues avoids
    // NPEs when tests call Android methods that return null in the
    // local JVM (vs. the real Android runtime).
    testOptions {
        unitTests {
            isReturnDefaultValues = true
            isIncludeAndroidResources = true
        }
    }

    buildFeatures {
        viewBinding = true
        buildConfig = true
    }

    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
        resources {
            excludes += setOf(
                "META-INF/AL2.0",
                "META-INF/LGPL2.1",
                "META-INF/*.kotlin_module"
            )
        }
    }

    signingConfigs {
        create("release") {
            // Plan 018: on GitHub Actions, signing.properties is always
            // created by the CI workflow (with real secrets OR a one-time
            // fallback keystore). If it's missing, fail loudly — don't
            // silently fall back to assembleDebug (which would produce an
            // APK with a different applicationId and break upgrades).
            val isGitHubAction = System.getenv("GITHUB_ACTIONS") == "true"
            val propertiesFilePath = if (isGitHubAction) {
                "/tmp/signing.properties"
            } else {
                System.getProperty("user.home") + "/.glm-android/signing.properties"
            }
            val propertiesFile = File(propertiesFilePath)
            if (propertiesFile.exists()) {
                val properties = Properties()
                properties.load(propertiesFile.inputStream())
                keyAlias = properties["keyAlias"] as String?
                keyPassword = properties["keyPassword"] as String?
                storeFile = (properties["storeFile"] as String?)?.let { File(it) }
                storePassword = properties["storePassword"] as String?
            } else if (isGitHubAction) {
                throw GradleException(
                    "GITHUB_ACTIONS=true but /tmp/signing.properties is missing. " +
                    "The CI workflow must create it before invoking Gradle."
                )
            }
        }
        // Use AGP's default debug signing config (auto-generated debug.keystore).
        // Do not override storeFile/storePassword here.
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            isShrinkResources = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = signingConfigs.getByName("release")
            resValue("string", "app_name", "GLM Proxy")
        }
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-DEBUG"
            resValue("string", "app_name", "GLM Proxy Debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.service)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.appcompat)
    implementation(libs.androidx.constraintlayout)
    implementation(libs.material)
    implementation(libs.androidx.webkit)
    implementation(libs.androidx.browser)
    implementation(libs.kotlinx.coroutines.android)

    // Plan 019: test dependencies. JUnit + Truth for assertions +
    // Robolectric for tests that touch Android framework classes.
    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.truth)
}
