//go:build !windows
// +build !windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// findPoEPath attempts to find the Path of Exile installation path
// Non-Windows version (stub)
func findPoEPath(config *Config, logger func(string, ...interface{})) string {
	// Method 1: Check if path is already in config
	if config.PoEPath != "" {
		if isValidPoEPath(config.PoEPath) {
			logger("Using PoE path from config: %s", config.PoEPath)
			return config.PoEPath
		}
		logger("Saved PoE path in config is invalid, searching for new path...")
	}

	// For non-Windows, just return empty and let user specify
	return ""
}

// isValidPoEPath checks if the given path contains Path of Exile installation
// by checking for Client.txt in logs folder
func isValidPoEPath(path string) bool {
	clientTxtPath := filepath.Join(path, "logs", "Client.txt")
	if _, err := os.Stat(clientTxtPath); err == nil {
		return true
	}
	// Also check if PathOfExile.exe exists
	exePath := filepath.Join(path, "PathOfExile.exe")
	if _, err := os.Stat(exePath); err == nil {
		return true
	}
	return false
}

// checkRegistry checks Windows registry for PoE installation path
// Non-Windows version (stub)
func checkRegistry() string {
	return ""
}

// getCommonPaths returns a list of common Path of Exile installation paths
// Non-Windows version (stub)
func getCommonPaths() []string {
	return []string{}
}

// searchForPoE searches for PathOfExile.exe or Client.txt in common locations
// Non-Windows version (stub)
func searchForPoE(timeout time.Duration) string {
	return ""
}

// getDriveLetters returns available drive letters on Windows
// Non-Windows version (stub)
func getDriveLetters() []string {
	return []string{}
}

// savePoEPathToConfig saves the PoE path to the config file
func savePoEPathToConfig(config *Config, poePath string) error {
	config.PoEPath = poePath
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("error getting config path: %w", err)
	}

	// Read existing config to preserve patterns
	configData, err := os.ReadFile(configPath)
	if err == nil {
		var existingConfig Config
		if err := json.Unmarshal(configData, &existingConfig); err == nil {
			config.Patterns = existingConfig.Patterns
		}
	}

	// Write updated config
	configJSON, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return fmt.Errorf("error marshalling config: %w", err)
	}

	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}

	return nil
}

// promptUserForPath prompts the user to enter the PoE installation path
func promptUserForPath() string {
	fmt.Println("\nCould not automatically find Path of Exile installation.")
	fmt.Println("Please enter the path to your Path of Exile installation")
	fmt.Println("(e.g., D:\\Games\\Path of Exile or C:\\Program Files (x86)\\Grinding Gear Games\\Path of Exile):")
	fmt.Print("Path: ")

	var path string
	fmt.Scanln(&path)
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"`) // Remove quotes if user added them

	// Normalize path separators
	path = filepath.Clean(path)

	return path
}
