# zteOnu Android

Native Android (Kotlin) front-end over the same Go core as the desktop tool.
The Go logic (`app/factory`, `app/crypto`, `app/telnet`, `app/onu`) is reused
unchanged; a thin gomobile binding (`/mobile`) exposes it to Kotlin.

## Architecture

```
mobile/zteonu.go   → gomobile bind → android/app/libs/zteonu.aar   (Go core)
android/app/...    → Kotlin UI (MainActivity) that calls the .aar
```

## Requirements

- Go (matches the repo's go.mod), plus `gomobile` and `gobind`
- Android SDK (platform 34, build-tools 34.0.0) + **NDK** (gomobile needs it)
- JDK 17
- Gradle 8.7 (or the Gradle bundled with Android Studio)

## Build (command line)

1. Install gomobile once:

   ```bash
   go install golang.org/x/mobile/cmd/gomobile@latest
   go install golang.org/x/mobile/cmd/gobind@latest
   ```

2. Install the NDK (example version), and point gomobile at it:

   ```bash
   sdkmanager "ndk;26.1.10909125"
   export ANDROID_NDK_HOME="$ANDROID_SDK_ROOT/ndk/26.1.10909125"
   ```

3. Build the Go library into the app:

   ```bash
   gomobile bind -target=android -androidapi 21 \
     -javapkg=com.zteonu.core \
     -o android/app/libs/zteonu.aar \
     ./mobile
   ```

4. Build the APK:

   ```bash
   cd android
   gradle assembleDebug          # or ./gradlew assembleDebug in Android Studio
   ```

   The APK is at `android/app/build/outputs/apk/debug/app-debug.apk`.

## Build (Android Studio)

Open the `android/` folder in Android Studio. Run step 3 above once to produce
`app/libs/zteonu.aar` (Studio does not run gomobile for you), then Build > Build
APK(s).

## Build (GitHub Actions, easiest)

Push the repo to GitHub and run the **android** workflow
(`.github/workflows/android.yml`, `workflow_dispatch` or a `v*` tag). It installs
the NDK + gomobile, runs the bind, builds the APK, and uploads it as the
`zteonu-android-apk` artifact.

## Notes / limitations

- **Client MAC**: Android does not let apps read the real Wi-Fi MAC, and the ONU
  authorizes by the actual L2 source MAC of the connection. So on Android you
  must enter the phone's Wi-Fi MAC for this network manually (Settings → Wi-Fi →
  the connected network → MAC/privacy) into the "自定义 MAC" field. "尝试读取本机
  MAC" is best-effort and usually blocked on modern Android.
- **Cleartext HTTP**: the webFac flow is plain HTTP on port 80, so the manifest
  sets `usesCleartextTraffic="true"`.
- Run the phone on the ONU's Wi-Fi/LAN so `192.168.1.1` is reachable.
