package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	// for embedding default config
	_ "embed"
)

//go:embed config/default-config.json
var defaultConfig []byte

func checkConfig() {
	configPath, err := getConfigPath()
	if err != nil {
		fmt.Println("Error getting config path:", err)
		os.Exit(1)
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// fmt.Println("Configuration file not found. Please create a config.json file at", path)
		// Use the default config to create the config file, if possible create the Notifier folder too
		if err := os.MkdirAll(path.Dir(configPath), 0755); err != nil {
			fmt.Println("Error creating Notifier directory:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(configPath, defaultConfig, 0644); err != nil {
			fmt.Println("Error creating config file:", err)
			os.Exit(1)
		}
	}
}

type Pattern struct {
	Name    string `json:"name"`
	Regex   string `json:"regex"`
	Beep    bool   `json:"beep"`
	Toast   bool   `json:"toast"`
	Message string `json:"message"`
}

type Config struct {
	Patterns []Pattern `json:"patterns"`
	PoEPath  string    `json:"poePath,omitempty"` // Path to Path of Exile installation directory
}

func importConfig() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, fmt.Errorf("error getting config path: %w", err)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}
	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("error unmarshalling config: %w", err)
	}
	return &config, nil
}

func getConfigPath() (string, error) {
	// This function should return the path to the configuration file.
	switch runtime.GOOS {
	case "windows":
		// For Windows, return the path to the config file in the user's Documents/My Games/Path of Exile/Notifier directory.
		user, _ := user.Current()
		return path.Join(user.HomeDir, "Documents", "My Games", "Path of Exile", "Notifier", "notifier_config.json"), nil
	case "linux", "darwin":
		return "", errors.New("unsupported operating system")
	default:
		// return error if the OS is not supported
		return "", errors.New("unknown operating system")
	}
}

// saveConfig saves the config to the config file
func saveConfig(config *Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("error getting config path: %w", err)
	}
	configData, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return fmt.Errorf("error marshalling config: %w", err)
	}
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}
	return nil
}

// findPoEInstallation searches for Path of Exile installation in common locations
// Returns the path to the installation directory if found, empty string otherwise
func findPoEInstallation(logger *log.Logger) string {
	if runtime.GOOS != "windows" {
		return ""
	}

	// Common installation paths to check
	commonPaths := []string{
		`C:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`C:\Program Files\Grinding Gear Games\Path of Exile`,
		`D:\Games\Path of Exile`,
		`E:\Games\Path of Exile`,
		`F:\Games\Path of Exile`,
		`C:\Games\Path of Exile`,
		`D:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`D:\Program Files\Grinding Gear Games\Path of Exile`,
		`E:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`E:\Program Files\Grinding Gear Games\Path of Exile`,
	}

	// Get user's home directory to check common user installation locations
	user, err := user.Current()
	if err == nil {
		userPaths := []string{
			filepath.Join(user.HomeDir, "Games", "Path of Exile"),
			filepath.Join(user.HomeDir, "Documents", "Games", "Path of Exile"),
			filepath.Join(user.HomeDir, "Desktop", "Path of Exile"),
		}
		commonPaths = append(commonPaths, userPaths...)
	}

	// Also check all available drives
	drives := []string{"C:", "D:", "E:", "F:", "G:", "H:"}
	for _, drive := range drives {
		commonPaths = append(commonPaths,
			filepath.Join(drive, "Program Files (x86)", "Grinding Gear Games", "Path of Exile"),
			filepath.Join(drive, "Program Files", "Grinding Gear Games", "Path of Exile"),
			filepath.Join(drive, "Games", "Path of Exile"),
		)
	}

	startTime := time.Now()
	timeout := 8 * time.Second // Max 8 seconds for searching

	// Files to look for to verify it's the PoE installation
	verificationFiles := []string{
		"PathOfExile.exe",
		"logs\\Client.txt",
		"logs/Client.txt",
	}

	// Search through paths with timeout
	for _, basePath := range commonPaths {
		if time.Since(startTime) > timeout {
			if logger != nil {
				logger.Printf("Search timeout reached, stopping search")
			}
			break
		}

		// Check if directory exists
		if info, err := os.Stat(basePath); err != nil || !info.IsDir() {
			continue
		}

		// Check for verification files
		for _, verifyFile := range verificationFiles {
			fullPath := filepath.Join(basePath, verifyFile)
			if _, err := os.Stat(fullPath); err == nil {
				// Found it! Normalize the path
				normalizedPath := filepath.Clean(basePath)
				// Convert to Windows-style path separators
				normalizedPath = strings.ReplaceAll(normalizedPath, "/", "\\")
				if logger != nil {
					logger.Printf("Found Path of Exile installation at: %s", normalizedPath)
				}
				return normalizedPath
			}
		}
	}

	if logger != nil {
		logger.Printf("Could not find Path of Exile installation in common locations")
	}
	return ""
}

// getClientLogPath returns the path to Client.txt based on the PoE installation path
func getClientLogPath(poePath string) string {
	if poePath == "" {
		return ""
	}
	return filepath.Join(poePath, "logs", "Client.txt")
}
