//go:build windows
// +build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// findPoEPath attempts to find the Path of Exile installation path
// It tries multiple methods:
// 1. Check if path is already saved in config
// 2. Check Windows registry
// 3. Check common installation paths
// 4. Search for PathOfExile.exe or Client.txt
// Returns the path if found, empty string otherwise
func findPoEPath(config *Config, logger func(string, ...interface{})) string {
	// Method 1: Check if path is already in config
	if config.PoEPath != "" {
		if isValidPoEPath(config.PoEPath) {
			logger("Using PoE path from config: %s", config.PoEPath)
			return config.PoEPath
		}
		logger("Saved PoE path in config is invalid, searching for new path...")
	}

	// Method 2: Check Windows registry
	if runtime.GOOS == "windows" {
		if path := checkRegistry(); path != "" {
			if isValidPoEPath(path) {
				logger("Found PoE path in registry: %s", path)
				return path
			}
		}
	}

	// Method 3: Check common installation paths
	commonPaths := getCommonPaths()
	for _, path := range commonPaths {
		if isValidPoEPath(path) {
			logger("Found PoE path in common location: %s", path)
			return path
		}
	}

	// Method 4: Search for PathOfExile.exe or Client.txt (with timeout)
	logger("Searching for PoE installation...")
	if path := searchForPoE(10 * time.Second); path != "" {
		logger("Found PoE path by searching: %s", path)
		return path
	}

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
func checkRegistry() string {
	if runtime.GOOS != "windows" {
		return ""
	}

	// Check common registry locations for PoE
	registryKeys := []struct {
		key   registry.Key
		path  string
		value string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Grinding Gear Games\Path of Exile`, "InstallLocation"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Grinding Gear Games\Path of Exile`, "InstallLocation"},
		{registry.CURRENT_USER, `SOFTWARE\Grinding Gear Games\Path of Exile`, "InstallLocation"},
	}

	for _, regKey := range registryKeys {
		key, err := registry.OpenKey(regKey.key, regKey.path, registry.READ)
		if err != nil {
			continue
		}

		value, _, err := key.GetStringValue(regKey.value)
		key.Close()
		if err == nil && value != "" {
			return value
		}
	}

	return ""
}

// getCommonPaths returns a list of common Path of Exile installation paths
func getCommonPaths() []string {
	paths := []string{
		`C:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`C:\Program Files\Grinding Gear Games\Path of Exile`,
		`D:\Games\Path of Exile`,
		`D:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`D:\Program Files\Grinding Gear Games\Path of Exile`,
		`E:\Games\Path of Exile`,
		`E:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`E:\Program Files\Grinding Gear Games\Path of Exile`,
		`F:\Games\Path of Exile`,
		`F:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`F:\Program Files\Grinding Gear Games\Path of Exile`,
	}

	// Add user's Documents path (sometimes PoE is installed there)
	if user, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(user, "Documents", "My Games", "Path of Exile"))
	}

	return paths
}

// searchForPoE searches for PathOfExile.exe or Client.txt in common locations
// with a timeout to avoid taking too long
func searchForPoE(timeout time.Duration) string {
	done := make(chan string, 1)
	stop := make(chan bool, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Ignore panics from file system access
			}
		}()

		// Get all drive letters on Windows
		drives := getDriveLetters()
		searchPaths := []string{}

		// Add common subdirectories to search in
		commonDirs := []string{
			filepath.Join("Grinding Gear Games", "Path of Exile"),
			filepath.Join("Games", "Path of Exile"),
			filepath.Join("Program Files (x86)", "Grinding Gear Games", "Path of Exile"),
			filepath.Join("Program Files", "Grinding Gear Games", "Path of Exile"),
		}

		for _, drive := range drives {
			for _, dir := range commonDirs {
				searchPaths = append(searchPaths, filepath.Join(drive, dir))
			}
		}

		// Search for PathOfExile.exe or Client.txt
		for _, searchPath := range searchPaths {
			select {
			case <-stop:
				return
			default:
				// Check for Client.txt first (faster)
				clientTxt := filepath.Join(searchPath, "logs", "Client.txt")
				if _, err := os.Stat(clientTxt); err == nil {
					done <- searchPath
					return
				}
				// Check for PathOfExile.exe
				exePath := filepath.Join(searchPath, "PathOfExile.exe")
				if _, err := os.Stat(exePath); err == nil {
					done <- searchPath
					return
				}
			}
		}
	}()

	select {
	case path := <-done:
		return path
	case <-time.After(timeout):
		stop <- true
		return ""
	}
}

// getDriveLetters returns available drive letters on Windows
func getDriveLetters() []string {
	if runtime.GOOS != "windows" {
		return []string{"C:"}
	}

	drives := []string{}
	for drive := 'A'; drive <= 'Z'; drive++ {
		drivePath := string(drive) + ":\\"
		if _, err := os.Stat(drivePath); err == nil {
			drives = append(drives, drivePath)
		}
	}
	return drives
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
