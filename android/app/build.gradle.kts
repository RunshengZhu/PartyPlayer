plugins {
    id("com.android.application")
}

android {
    namespace = "com.partyplayer.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.partyplayer.app"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        // 由 build-apk.sh 注入时间版本号，如 v0.1.0-20260920-1830
        versionName = project.findProperty("appVersionName") as String? ?: "0.1.0"
        ndk {
            abiFilters += listOf("arm64-v8a")
        }
    }

    // jniLibs 解压到 nativeLibraryDir，Go 服务器二进制以 libpartyplayer.so
    // 形式携带并在运行时 exec（Android 10+ 仅允许执行该目录）。
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }
}
