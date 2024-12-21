# Clipy

Clipy is a bidirectional clipboard synchronization tool that allows seamless clipboard sharing between your computer and Android device. The tool consists of a lightweight server written in Go and a native Android app. It enables clipboard sync via WebSocket communication, ensuring that content copied on one device can be easily accessed and pasted on the other.

## Features

- **Bidirectional Clipboard Sync**: Copy and paste seamlessly between Android and PC.
- **Lightweight**: Optimized Android app and Go-based server.
- **Background Sync**: Clipboard data is synced in the background, with foreground notifications on Android for service persistence.
- **Share Functionality**: Easily share clipboard content from Android to PC without opening the app.
- **Cross-Platform**: Works on both Android and PC, with future plans for additional platform support.

## Architecture

The Clipy project consists of three main components:

1. **Server** (Go):
   - Handles WebSocket communication between devices.
   - Synchronizes clipboard content between Android and PC.
   
2. **Android Client** (Kotlin):
   - Provides a simple UI to toggle clipboard sync.
   - Implements services to keep the clipboard sync running in the background.

3. **UI**:
   - Simple user interfaces for both the PC client (running the server) and the Android app.
   - Allows starting and stopping clipboard synchronization and viewing sync status.

## Installation

### PC (Server)

1. Clone the repository:
   ```bash
   git clone https://github.com/aryanpnd/clipy.git
   cd clipy/server
   ```

2. Install Go (if you don’t have it installed yet):
   - Follow the instructions at [https://go.dev/doc/install](https://go.dev/doc/install).

3. Build the Go server:
   ```bash
   go build -o clipy-server .
   ```

4. Run the server:
   ```bash
   ./clipy-server
   ```

The server will automatically start and listen for WebSocket connections from the Android device.

### Android Client

The Android client allows you to connect to the PC server and sync your devices over your local host. You can find the Android client repository here: [Clipy Android Client](https://github.com/aryanpnd/clipy-client-android).

1. Open the Android project in [Android Studio](https://developer.android.com/studio).
2. Build and install the app on your Android device.
3. Grant necessary permissions for clipboard access and notifications.
4. Start the clipboard sync via the app, connecting to the server running on your PC.

### Configuration

In the `build.gradle` file for the Android project, the following configuration should be set:

```gradle
android {
    compileSdkVersion 34
    defaultConfig {
        applicationId "com.example.clipy"
        minSdkVersion 34
        targetSdkVersion 34
    }
    ...
}

dependencies {
    implementation 'androidx.core:core-ktx:1.10.0'
    implementation 'androidx.appcompat:appcompat:1.7.0'
    implementation 'com.google.android.material:material:1.9.0'
    implementation 'androidx.activity:activity-ktx:1.7.0'
    implementation 'androidx.constraintlayout:constraintlayout:2.1.4'
    implementation 'com.squareup.okhttp3:okhttp:4.10.0'
    testImplementation 'junit:junit:4.13.2'
    androidTestImplementation 'androidx.test.ext:junit:1.1.5'
    androidTestImplementation 'androidx.test.espresso:espresso-core:3.5.1'
}
```

### Permissions Required

The Android app requires the following permissions:
- `SYSTEM_ALERT_WINDOW`
- `INTERNET`
- `FOREGROUND_SERVICE`
- `POST_NOTIFICATIONS`
- `READ_CLIPBOARD`

## Usage

1. **Starting the Sync**: After installing the Android app and starting the server, click the 'Start Clipboard Sync' button in the app to initiate synchronization.
2. **Sharing Clipboard Data**: On Android, use the 'Share' functionality to send clipboard data to the PC without opening the app.
3. **Stopping the Sync**: The clipboard sync can be stopped by clicking the 'Stop Clipboard Sync' button in the app.

## Future Features

- **Multi-platform support**: Adding support for additional platforms such as macOS or Linux.
- **Performance optimization**: Refactoring the server into a more performance-efficient language like Go or Rust if needed.
- **Customizable UI**: Providing options for users to customize how clipboard sync is handled.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---