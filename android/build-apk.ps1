# Builds the Android APK: gomobile bind (Go core -> .aar) then Gradle assemble.
# Requires: Go + gomobile/gobind on PATH, Android SDK+NDK, JDK 17, and gradle
# (or Android Studio's gradle). Run from the repo root or the android/ folder.
#
#   powershell -File android/build-apk.ps1
#
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $repo

$ndkVersion = "26.1.10909125"
$sdk = $env:ANDROID_HOME
if (-not $sdk) { $sdk = $env:ANDROID_SDK_ROOT }
if (-not $sdk) { throw "ANDROID_HOME / ANDROID_SDK_ROOT not set" }

$ndk = Join-Path $sdk "ndk\$ndkVersion"
if (-not (Test-Path (Join-Path $ndk "source.properties"))) {
    Write-Host "Installing NDK $ndkVersion ..."
    $sm = Join-Path $sdk "cmdline-tools\latest\bin\sdkmanager.bat"
    ("y`r`n" * 10) | & $sm "ndk;$ndkVersion"
}
$env:ANDROID_NDK_HOME = $ndk

# gomobile on PATH?
$gopathBin = Join-Path (go env GOPATH) "bin"
if ($env:PATH -notlike "*$gopathBin*") { $env:PATH = "$gopathBin;$env:PATH" }
if (-not (Get-Command gomobile -ErrorAction SilentlyContinue)) {
    Write-Host "Installing gomobile ..."
    go install golang.org/x/mobile/cmd/gomobile@latest
    go install golang.org/x/mobile/cmd/gobind@latest
}

Write-Host "gomobile bind -> android/app/libs/zteonu.aar"
New-Item -ItemType Directory -Force -Path "android/app/libs" | Out-Null
gomobile bind -target=android -androidapi 21 -javapkg=com.zteonu.core -o "android/app/libs/zteonu.aar" ./mobile

Write-Host "Building APK ..."
Set-Location (Join-Path $repo "android")
if (Get-Command gradle -ErrorAction SilentlyContinue) {
    gradle assembleDebug --no-daemon
} elseif (Test-Path ".\gradlew.bat") {
    .\gradlew.bat assembleDebug
} else {
    throw "No gradle found. Install Gradle 8.7, or open the android/ project in Android Studio and Build > Build APK(s)."
}

Write-Host "APK: android/app/build/outputs/apk/debug/app-debug.apk"
