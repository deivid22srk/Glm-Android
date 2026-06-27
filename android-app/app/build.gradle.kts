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
            // On GitHub Actions, signing.properties is created at /tmp/signing.properties
            // from repository secrets. Locally, fall back to a debug-equivalent keystore.
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
            }
        }
        getByName("debug") {
            storeFile = file(layout.buildDirectory.dir("../testkey.keystore"))
            storePassword = "testkey"
            keyAlias = "testkey"
            keyPassword = "testkey"
        }
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
    implementation(libs.androidx.webkit)
    implementation(libs.material)
}
