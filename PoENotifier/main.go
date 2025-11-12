package main

import (
	"context"
	"fmt"
	"github.com/go-faster/tail"
	"io"
	"log"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	// Setup logging
	initSystray()
	logger := setupLogging()
	logger.Println("Starting PoE Notifier...")

	// Will check if the config file exists, if not it will create it with the default config
	// User can edit the config file to change the patterns to match
	checkConfig()

	// Load config
	config, err := importConfig()
	if err != nil {
		logger.Printf("Error importing config: %v", err)
		return
	}
	logger.Println("Config checked and loaded")

	// Determine Path of Exile installation path
	var poePath string
	if config.PoEPath != "" {
		// Use path from config if available
		poePath = config.PoEPath
		logger.Printf("Using Path of Exile path from config: %s", poePath)
		// Verify the path is still valid
		clientLogPath := getClientLogPath(poePath)
		if _, err := os.Stat(clientLogPath); err != nil {
			logger.Printf("Warning: Client.txt not found at configured path. Searching for installation...")
			poePath = "" // Clear invalid path to trigger search
		}
	}

	// If no valid path in config, search for installation
	if poePath == "" {
		logger.Println("Searching for Path of Exile installation...")
		poePath = findPoEInstallation(logger)
		if poePath != "" {
			// Save found path to config
			config.PoEPath = poePath
			if err := saveConfig(config); err != nil {
				logger.Printf("Warning: Could not save PoE path to config: %v", err)
			} else {
				logger.Printf("Saved Path of Exile path to config: %s", poePath)
			}
		}
	}

	// If still no path found, show error and exit
	if poePath == "" {
		configPath, _ := getConfigPath()
		logger.Printf("ERROR: Could not find Path of Exile installation.")
		logger.Printf("Please edit the config file and set 'poePath' to your Path of Exile installation directory.")
		logger.Printf("Config file location: %s", configPath)
		logger.Printf("Example: \"poePath\": \"D:\\\\Games\\\\Path of Exile\"")
		
		// Show a message box on Windows
		if runtime.GOOS == "windows" {
			showErrorMessage("Path of Exile Not Found", 
				fmt.Sprintf("Could not find Path of Exile installation.\n\nPlease edit the config file and set 'poePath' to your installation directory.\n\nConfig file: %s", configPath))
		}
		os.Exit(1)
	}

	// Get the Client.txt path
	clientLogPath := getClientLogPath(poePath)
	logger.Printf("Using Client.txt at: %s", clientLogPath)

	// Verify the log file exists
	if _, err := os.Stat(clientLogPath); err != nil {
		logger.Printf("ERROR: Client.txt not found at: %s", clientLogPath)
		logger.Printf("Please verify that Path of Exile is installed at: %s", poePath)
		os.Exit(1)
	}

	t := tail.File(clientLogPath, tail.Config{
		Follow:     true,       // tail -f
		BufferSize: 1024 * 128, // 128 kb for internal reader buffer

		NotifyTimeout: time.Duration(1 * time.Second),

		Location: &tail.Location{Whence: io.SeekEnd, Offset: 0},
	})
	ctx := context.Background()

	logger.Printf("Config imported successfully. Found %d patterns:", len(config.Patterns))
	for _, pattern := range config.Patterns {
		logger.Printf("  - Pattern: %s, Regex: %s", pattern.Name, pattern.Regex)
	}

	logger.Println("Starting to tail PoE log file...")

	if err := t.Tail(ctx, func(ctx context.Context, l *tail.Line) error {
		if matched, pattern := checkPattern(string(l.Data), config.Patterns, logger); matched {
			logger.Printf("PATTERN MATCHED: %s - Line: %s", pattern.Name, string(l.Data))
			if pattern.Toast {
				showToast(pattern.Name, pattern.Message)
			}
			if pattern.Beep {
				beep()
			}
		}
		return nil
	}); err != nil {
		logger.Printf("Fatal error in tail: %v", err)
		panic(err)
	}
}

func checkPattern(line string, patterns []Pattern, logger *log.Logger) (bool, Pattern) {
	for _, pattern := range patterns {
		cleanRegex := pattern.Regex
		if len(cleanRegex) > 2 && cleanRegex[0] == '`' && cleanRegex[len(cleanRegex)-1] == '`' {
			cleanRegex = cleanRegex[1 : len(cleanRegex)-1]
		}

		if matched, err := regexp.MatchString(cleanRegex, line); err != nil {
			logger.Printf("Regex error for pattern '%s': %v", pattern.Name, err)
		} else if matched {
			return true, pattern
		}
	}
	return false, Pattern{}
}

// setupLogging creates a logger that writes to both file and console
func setupLogging() *log.Logger {
	// Create logs directory if it doesn't exist
	user, _ := user.Current()
	logDir := path.Join(user.HomeDir, "Documents", "My Games", "Path of Exile", "Notifier", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("Failed to create logs directory: %v\n", err)
		return log.New(os.Stdout, "[PoENotifier] ", log.LstdFlags|log.Lshortfile)
	}

	// Create log file with timestamp
	logFileName := fmt.Sprintf("poe_notifier_%s.log", time.Now().Format("2006-01-02"))
	logFilePath := filepath.Join(logDir, logFileName)

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("Failed to open log file: %v\n", err)
		return log.New(os.Stdout, "[PoENotifier] ", log.LstdFlags|log.Lshortfile)
	}

	// Create a multi-writer to write to both file and console
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	// Create logger with timestamp, filename and line number
	logger := log.New(multiWriter, "[PoENotifier] ", log.LstdFlags|log.Lshortfile)

	logger.Printf("Logging initialized. Log file: %s", logFilePath)
	return logger
}

// beep plays a system beep sound.
// Only supported on Windows at the moment.
func beep() {
	if runtime.GOOS != "windows" {
		// Windows beep
		fmt.Print("\a")
	} else {
		kernel32 := syscall.NewLazyDLL("user32.dll")
		kernel32.NewProc("MessageBeep").Call(880, 200)
	}
}

// showErrorMessage shows a Windows message box with an error message
func showErrorMessage(title, message string) {
	if runtime.GOOS != "windows" {
		fmt.Printf("%s: %s\n", title, message)
		return
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	
	// Convert strings to UTF-16
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	
	// MessageBoxW(hWnd, lpText, lpCaption, uType)
	// MB_OK | MB_ICONERROR = 0x00000010 | 0x00000040 = 0x00000050
	messageBox.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), 0x00000050)
}
