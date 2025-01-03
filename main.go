package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
	"golang.design/x/clipboard"
)

var (
	clients               = make(map[*websocket.Conn]bool)
	clientsMutex          sync.Mutex
	lastClipboardContent  string
	isServerRunning       = false
	paused                = false // A flag to control pause/resume
	stopMonitoring        = make(chan bool)
	httpServer            *http.Server      // HTTP server instance
	notificationsEnabled  = true            // Flag to enable/disable notifications
	notificationsMenuItem *systray.MenuItem // Menu item to toggle notifications
)

var statusMenuItem *systray.MenuItem
var connectedDevicesMenuItem *systray.MenuItem

const (
	port = "8080"
)

// Start the application
func main() {
	// go func() {
	// 	sendNotification("Clipy", "Checking if another instance is running or port is busy...")
	// }()
	if isPortInUse(port) {
		sendNotification("Clipy", "The port 8080 is busy. Close the application using the port to start the application again.")
		// try removing the lock file if exists
		os.Exit(1)
	}

	// Start the system tray and wait for it to exit
	go startSystemTray()
	// Block main goroutine to keep the application alive
	select {}
}
func isPortInUse(port string) bool {
	// Try to listen on the specified port
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		// Port is in use
		return true
	}
	ln.Close()
	return false
}

// Start the system tray
func startSystemTray() {
	systray.Run(onReady, onExit)
}

// Initialize system tray options
func onReady() {
	systray.SetIcon(getIcon())
	systray.SetTitle("Clipboard Sync")
	systray.SetTooltip("Clipboard Sync Server")

	// Add the new status items
	statusMenuItem = systray.AddMenuItem("Server Status: Paused", "Displays the current status of the server")
	connectedDevicesMenuItem = systray.AddMenuItem("Connected Devices: 0", "Displays the number of connected devices")

	// Add menu items
	startMenuItem := systray.AddMenuItem("Start sync", "Start the Clipboard Sync server")
	stopMenuItem := systray.AddMenuItem("Stop sync", "Stop the Clipboard Sync server")
	openQRMenuItem := systray.AddMenuItem("Open QR", "Open the QR code page in browser")

	// Add the toggle notifications button
	notificationsMenuItem = systray.AddMenuItem("Disable Notifications", "Toggle notifications on/off")

	// Add the exit button
	exitMenuItem := systray.AddMenuItem("Exit", "Exit the application")

	// Start the server on first launch
	startServer()

	// Initially disable the stop button and enable the start button based on server state
	updateMenuItemsState(startMenuItem, stopMenuItem)

	// Handle menu item clicks
	go func() {
		for {
			select {
			case <-startMenuItem.ClickedCh:
				fmt.Println("[INFO] Start menu clicked")
				if paused {
					resumeServer() // Resume the server if it's paused
				} else {
					startServer() // Start the server if it's not running
				}
				// Update the state of the menu items after starting the server
				updateMenuItemsState(startMenuItem, stopMenuItem)

			case <-stopMenuItem.ClickedCh:
				fmt.Println("[INFO] Stop menu clicked")
				stopServer() // Pause the server
				// Update the state of the menu items after stopping the server
				updateMenuItemsState(startMenuItem, stopMenuItem)

			case <-openQRMenuItem.ClickedCh:
				fmt.Println("[INFO] Open QR menu clicked")
				openQRCodePage()

			case <-exitMenuItem.ClickedCh:
				fmt.Println("[INFO] Exit menu clicked")
				onExit() // Cleanup and exit the application
				// os.Exit(0) // Exit the application
				return

			case <-notificationsMenuItem.ClickedCh:
				toggleNotifications() // Toggle notifications on or off
			}
		}
	}()
}

// Function to update the status in the menu
func updateServerStatus() {
	if paused {
		statusMenuItem.SetTitle("Server Status: Paused")
	} else if isServerRunning {
		statusMenuItem.SetTitle("Server Status: Running")
	} else {
		statusMenuItem.SetTitle("Server Status: Stopped")
	}
}

// Function to update the number of connected devices
func updateConnectedDevices() {
	clientsMutex.Lock()
	connectedDevicesMenuItem.SetTitle(fmt.Sprintf("Connected Devices: %d", len(clients)))
	clientsMutex.Unlock()
}

// Update the menu items' enabled/disabled state
func updateMenuItemsState(startMenuItem, stopMenuItem *systray.MenuItem) {
	if paused {
		startMenuItem.Enable() // Enable "Start" button if server is paused
		stopMenuItem.Disable() // Disable "Stop" button if server is paused
	} else if isServerRunning {
		startMenuItem.Disable() // Disable "Start" button if server is running
		stopMenuItem.Enable()   // Enable "Stop" button if server is running
	} else {
		startMenuItem.Enable() // Enable "Start" button if the server is not running
		stopMenuItem.Disable() // Disable "Stop" button if the server is not running
	}

	// Update the status and connected devices info
	updateServerStatus()
	updateConnectedDevices()
}

// Start the WebSocket server and clipboard monitoring
func startServer() {
	if isServerRunning {
		fmt.Println("[INFO] Server is already running")
		sendNotification("Running", "Server is already running")
		return
	}
	if err := clipboard.Init(); err != nil {
		fmt.Printf("Failed to initialize clipboard: %v", err)
	}

	isServerRunning = true
	fmt.Println("[INFO] Starting server and clipboard monitoring")

	// Reinitialize channels
	stopMonitoring = make(chan bool)

	// Start WebSocket server and clipboard monitoring in separate goroutines
	go startWebSocketServer()
	go monitorClipboardChanges()

	// Optionally open QR page after server starts
	openQRCodePage()
}

// Stop the clipboard monitoring (pause)
func stopServer() {
	if !isServerRunning {
		fmt.Println("[INFO] Server is not running")
		return
	}

	paused = true // Set the flag to paused
	fmt.Println("[INFO] Pausing server and clipboard monitoring")

	// Send stop signal to clipboard monitoring
	stopMonitoring <- true
	sendNotification("Paused", "Clipboard syncing paused")
}

// Resume clipboard monitoring
func resumeServer() {
	if !isServerRunning || !paused {
		fmt.Println("[INFO] Server is already running or not paused")
		return
	}

	paused = false // Set the flag to resume
	fmt.Println("[INFO] Resuming clipboard monitoring")

	// Start clipboard monitoring again
	go monitorClipboardChanges()
	sendNotification("Resumed", "Clipboard syncing resumed")
}

// Start the WebSocket server
func startWebSocketServer() {
	// Get the specific local IP address
	ip := getLocalIP()
	if ip == "" {
		fmt.Println("[ERROR] Could not determine local IP address")
		return
	}

	// Start the WebSocket server on the local IP address
	address := fmt.Sprintf("%s:8080", ip)
	fmt.Printf("[INFO] Starting WebSocket server on ws://%s/ws\n", address)

	// Create a new WebSocket upgrader with custom origin check
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// Create a new HTTP multiplexer and handle WebSocket connections
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			fmt.Println("[ERROR] WebSocket upgrade error:", err)
			return
		}
		defer conn.Close()

		clientsMutex.Lock()
		clients[conn] = true
		clientsMutex.Unlock()

		// Update the number of connected devices
		updateConnectedDevices()

		fmt.Printf("[INFO] Client connected. Total clients: %d\n", len(clients))
		sendNotification("Device Connected", "Total devices: "+fmt.Sprint(len(clients)))

		// Handle WebSocket messages
		for {
			// Check for paused state
			if paused {
				time.Sleep(1 * time.Second) // Wait while paused
				continue
			}

			select {
			case <-stopMonitoring:
				fmt.Println("[INFO] WebSocket server shutting down.")
				return
			default:
				_, message, err := conn.ReadMessage()
				if err != nil {
					// Client disconnected or error reading message
					clientsMutex.Lock()
					delete(clients, conn) // Remove client from the map
					clientsMutex.Unlock()

					// Update the number of connected devices
					updateConnectedDevices()

					// Send notification if the client is disconnected
					fmt.Printf("[INFO] Client disconnected. Total clients: %d\n", len(clients))
					sendNotification("Device Disconnected", "Total devices: "+fmt.Sprint(len(clients)))
					return // Break the loop once the client disconnects
				}

				// Process received message
				content := string(message)
				fmt.Printf("[INFO] Clipboard received from client: %s\n", content)

				if strings.HasPrefix(content, "text:") {
					textContent := strings.TrimPrefix(content, "text:")

					if textContent != lastClipboardContent {
						err := clipboard.Write(clipboard.FmtText, []byte(textContent))
						if err != nil {
							fmt.Println("[ERROR] Failed to update clipboard text:", err)
						} else {
							lastClipboardContent = textContent
							fmt.Println("Clipboard updated with content:", textContent)
						}
					}
				} else if strings.HasPrefix(content, "image:") {
					// Handle image content (Base64-encoded)
					imageContent := strings.TrimPrefix(content, "image:")

					// Decode the Base64-encoded image
					decodedImage, err := base64.StdEncoding.DecodeString(imageContent)
					if err != nil {
						fmt.Printf("[ERROR] Failed to decode image: %v\n", err)
						sendNotification("Image Error", "Wrong image format received. Must be PNG.")
						continue
					}

					// Save the image to a file
					outputFile, err := saveImageToFile(decodedImage)
					if err != nil {
						fmt.Printf("[ERROR] Failed to save image to file: %v\n", err)
						sendNotification("Image Error", "Failed to save image to file. Must be PNG")
						continue
					}
					fmt.Printf("[INFO] Image saved to: %s\n", outputFile)

					// Send a notification
					sendNotification("Image Received", "Image saved to the Clipboard and Desktop.")

					// Save the image to the clipboard
					changed := clipboard.Write(clipboard.FmtImage, decodedImage)
					if changed == nil {
						fmt.Println("[ERROR] Failed to write image to clipboard")
						sendNotification("Image Error", "Failed to copy image to clipboard.")
						continue
					}
					<-changed // Wait for the write operation to complete
					fmt.Println("[INFO] Image successfully copied to clipboard.")
				}
			}
		}
	})

	// Create and start the HTTP server
	httpServer = &http.Server{Addr: address, Handler: mux}
	err := httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Println("[ERROR] WebSocket server error:", err)
	}
}

func monitorClipboardChanges() {
	lastClipboardContent = readClipboard()
	fmt.Print("[INFO] Initial clipboard content: ", lastClipboardContent, "\n")

	for {
		select {
		case <-stopMonitoring:
			fmt.Println("[INFO] Clipboard monitoring paused")
			return
		default:
			if paused {
				time.Sleep(1 * time.Second) // Just wait when paused
				continue
			}

			currentContent := readClipboard()
			if currentContent != lastClipboardContent {
				// fmt.Printf("[INFO] Clipboard updated locally: %s\n", currentContent)
				broadcastClipboard(currentContent, "local", nil)
				lastClipboardContent = currentContent
			}
			time.Sleep(1 * time.Second)
		}
	}
}

// Broadcast clipboard updates to all connected clients except the source
func broadcastClipboard(content, source string, sourceConn *websocket.Conn) {
	// Prevent sending the same content repeatedly
	if source == "server" && content == lastClipboardContent {
		fmt.Println("[INFO] Skipping broadcast, same content as last update.")
		return
	}

	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	for client := range clients {
		if client == sourceConn {
			continue // Skip broadcasting to the source client
		}

		err := client.WriteMessage(websocket.TextMessage, []byte(content))
		if err != nil {
			fmt.Printf("[ERROR] Failed to send message to client: %v\n", err)
			client.Close()
			delete(clients, client)
		}
	}
	fmt.Printf("[INFO] Broadcasted clipboard update to %d clients\n", len(clients))
}

func readClipboard() string {
	text := clipboard.Read(clipboard.FmtText)
	if text != nil && len(text) > 0 {
		return fmt.Sprintf("text:%s", string(text))
	}

	// Read image data from the clipboard
	imageData := clipboard.Read(clipboard.FmtImage)
	if imageData != nil && len(imageData) > 0 {
		// Decode the image from the byte slice
		img, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			fmt.Println("[ERROR] Failed to decode image:", err)
			return ""
		}
		// Encode the image as Base64
		var buf bytes.Buffer
		err = png.Encode(&buf, img)
		if err != nil {
			fmt.Println("[ERROR] Failed to encode image:", err)
			return ""
		}
		encodedImage := base64.StdEncoding.EncodeToString(buf.Bytes())
		return fmt.Sprintf("image:%s", encodedImage)
	}

	return ""
}

var qrRouteRegistered = false

func openQRCodePage() {
	// Register the /qr route only once
	if !qrRouteRegistered {
		ip := getLocalIP()
		wsURL := fmt.Sprintf("ws://%s:8080/ws", ip)
		qrCode, err := qrcode.Encode(wsURL, qrcode.Medium, 256)
		if err != nil {
			fmt.Println("[ERROR] Failed to generate QR code:", err)
			return
		}

		// Register the route for QR page
		http.HandleFunc("/qr", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html>
				<html lang="en">
					<head>
						<meta charset="UTF-8">
						<meta name="viewport" content="width=device-width, initial-scale=1.0">
						<title>Clipy - Sync your clipboard</title>
						<style>
						body {
							background-color: #171717;
							color: white;
							display: flex;
							flex-direction: column;
							align-items: center;
							justify-content: center;
							height: 100vh;
							margin: 0;
							font-family: Arial, sans-serif;
						}

						.header {
							text-align: center;
							margin-bottom: 30px;
						}

						.header h1 {
							font-size: 2.5rem;
							margin: 0;
						}

						.header p {
							font-size: 1rem;
							margin: 5px 0;
							color: #ccc;
						}

						.content {
							text-align: center;
						}

						p {
							margin: 10px 0;
						}

						.note {
							font-size: 0.9rem;
							color: #aaa;
							margin-top: 15px;
						}

						code {
							background-color: #333;
							padding: 5px;
							border-radius: 4px;
						}

						/* Footer */
						.footer {
							position: fixed;
							bottom: 10px;
							right: 10px;
							color: #ccc;
							font-size: 0.8rem;
						}

						.footer a {
							color: #ccc;
							margin-left: 5px;
						}

						.footer a:hover {
							text-decoration: underline;
						}
						</style>
					</head>
					<body>
						<div class="header">
						<h1>Clipy</h1>
						<p>Sync your clipboard effortlessly</p>
						</div>
						<div class="content">
						<p>Connect your Android device using the WebSocket URL or scan the QR code below:</p>
						<p><strong>WebSocket URL:</strong> <code>%s</code></p>
						<img src="data:image/png;base64,%s" alt="QR Code">
						<p class="note">You can use it using your system tray.</p>
						<p class="note">The clipboard images will be saved to <code>YOUR_Desktop\clipy</code>. Note: Except .PNG all formats would be ignored. </p>
						</div>

						<!-- Footer with GitHub link -->
						<div class="footer">
						<span>&copy; 2024 Clipy. Developed by</span>
						<a href="https://github.com/aryanpnd" target="_blank">aryan</a>
						<span>|</span>
						<a href="https://github.com/aryanpnd/clipy-client-pc" target="_blank">Contribute</a>
						</div>
					</body>
				</html>

			`, wsURL, base64.StdEncoding.EncodeToString(qrCode))
		})

		// Mark the route as registered
		qrRouteRegistered = true
	}

	// Start the QR server in a new goroutine to serve the page
	go startQRCodeServer()

	// Open the QR code page in the browser using a unique URL path
	ip := "localhost"
	err := exec.Command(getBrowserCommand(), "/c", "start", fmt.Sprintf("http://%s:3000/qr", ip)).Start()
	if err != nil {
		fmt.Println("[ERROR] Failed to open QR code page:", err)
	}
}

// Start an HTTP server on port 3000 to serve the QR code page
func startQRCodeServer() {
	httpServer := &http.Server{
		Addr:    ":3000", // Listen on port 3000
		Handler: nil,     // Use default mux
	}

	fmt.Println("[INFO] Starting HTTP server on http://localhost:3000")
	if err := httpServer.ListenAndServe(); err != nil {
		fmt.Println("[ERROR] HTTP server error:", err)
	}
}

// Utility to get the default browser command based on OS
func getBrowserCommand() string {
	switch runtime.GOOS {
	case "windows":
		return "C:\\Windows\\System32\\cmd.exe"
	case "darwin":
		return "open"
	default:
		return "xdg-open"
	}
}

// Utility to get local IP address (WLAN adapter)
func getLocalIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("[ERROR] Failed to retrieve network interfaces:", err)
		return "127.0.0.1"
	}

	for _, iface := range interfaces {
		// Check if the interface is up and not a loopback interface
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			addrs, err := iface.Addrs()
			if err != nil {
				fmt.Println("[ERROR] Failed to retrieve addresses for interface:", iface.Name, err)
				continue
			}

			for _, addr := range addrs {
				// Parse the address and check if it's IPv4
				if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					// Ensure we get the Wi-Fi adapter's IP
					if strings.Contains(iface.Name, "Wi-Fi") || strings.Contains(iface.Name, "wlan") {
						return ipNet.IP.String()
					}
				}
			}
		}
	}

	// Fallback to localhost if no valid IP is found
	return "127.0.0.1"
}

// Utility to get tray icon (reads icon file and returns it as byte slice)
func getIcon() []byte {
	// Load the icon from a file (ensure the path to the icon is correct)
	iconPath := "clipylogo.ico" // Replace with your actual file path

	iconFile, err := os.Open(iconPath)
	if err != nil {
		fmt.Println("[ERROR] Failed to load icon:", err)
		return nil
	}
	defer iconFile.Close()

	// Read the icon file into a byte slice
	iconBytes, err := io.ReadAll(iconFile)
	if err != nil {
		fmt.Println("[ERROR] Failed to read icon:", err)
		return nil
	}

	return iconBytes
}

// Send a notification if notifications are enabled
func sendNotification(title, message string) {
	if notificationsEnabled {
		err := beeep.Notify(title, message, "clipylogo.png")
		if err != nil {
			fmt.Printf("[ERROR] Unable to send notification: %v\n", err)
		}
	}
}

// Function to toggle notifications on or off
func toggleNotifications() {
	if notificationsEnabled {
		notificationsEnabled = false
		notificationsMenuItem.SetTitle("Enable Notifications")
		sendNotification("Notifications Disabled", "Notifications have been turned off.")
	} else {
		notificationsEnabled = true
		notificationsMenuItem.SetTitle("Disable Notifications")
		sendNotification("Notifications Enabled", "Notifications have been turned on.")
	}
}

// Cleanup on exit
func onExit() {
	// Stop clipboard monitoring by sending stop signal
	stopMonitoring <- true

	// Close WebSocket connections (close each client connection)
	clientsMutex.Lock()
	for client := range clients {
		err := client.Close()
		if err != nil {
			fmt.Printf("[ERROR] Failed to close client connection: %v\n", err)
		} else {
			fmt.Println("[INFO] Client connection closed.")
		}
	}
	clientsMutex.Unlock()

	// Close the HTTP server (shuts down WebSocket server as well)
	if httpServer != nil {
		err := httpServer.Close()
		if err != nil {
			fmt.Printf("[ERROR] Failed to close HTTP server: %v\n", err)
		} else {
			fmt.Println("[INFO] HTTP server stopped successfully.")
		}
	}

	fmt.Println("[INFO] Server stopped successfully.")

	os.Exit(0)
}

// Saves the image to the Desktop inside the "clipy" folder and returns the file path
func saveImageToFile(decodedImage []byte) (string, error) {
	// Create a file from the decoded image
	imgReader := bytes.NewReader(decodedImage)
	img, _, err := image.Decode(imgReader)
	if err != nil {
		return "", fmt.Errorf("failed to decode image from bytes: %v", err)
	}

	// Get the desktop directory
	desktopDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to get user home directory: %v", err)
	}
	desktopDir = filepath.Join(desktopDir, "Desktop")

	// Create the "clipy" folder on the desktop if it doesn't exist
	clipyFolder := filepath.Join(desktopDir, "clipy")
	err = os.MkdirAll(clipyFolder, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create 'clipy' folder: %v", err)
	}

	// Create a unique filename for the image in the format "DD-MM-YY_HHMMSS_clipboard_image.png"
	t := time.Now()
	outputFile := filepath.Join(clipyFolder, fmt.Sprintf("%02d-%02d-%02d_%02d%02d%02d_clipboard_image.png",
		t.Day(), t.Month(), t.Year()%100, t.Hour(), t.Minute(), t.Second()))

	// Save the image as PNG to the file
	file, err := os.Create(outputFile)
	if err != nil {
		return "", fmt.Errorf("failed to create image file: %v", err)
	}
	defer file.Close()

	// Encode the image as PNG and save it to the file
	err = png.Encode(file, img)
	if err != nil {
		return "", fmt.Errorf("failed to encode and save image: %v", err)
	}

	fmt.Println("[INFO] Image saved to:", outputFile)
	return outputFile, nil
}
