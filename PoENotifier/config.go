package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
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
	GamePath string    `json:"game_path"` // Path to PoE installation directory
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

// validatePoEPath checks if a given path contains a valid PoE installation
// by looking for Client.txt or PathOfExile.exe
func validatePoEPath(basePath string) (string, bool) {
	// Check for Client.txt in logs folder
	clientTxtPath := filepath.Join(basePath, "logs", "Client.txt")
	if _, err := os.Stat(clientTxtPath); err == nil {
		return clientTxtPath, true
	}
	
	// Also check if PathOfExile.exe exists as a validation
	exePath := filepath.Join(basePath, "PathOfExile.exe")
	if _, err := os.Stat(exePath); err == nil {
		// If exe exists, the Client.txt might not exist yet, create logs path
		return clientTxtPath, true
	}
	
	// Check for PathOfExile_x64.exe (newer versions)
	exePath64 := filepath.Join(basePath, "PathOfExile_x64.exe")
	if _, err := os.Stat(exePath64); err == nil {
		return clientTxtPath, true
	}
	
	return "", false
}

// findPoEInstallation searches for Path of Exile installation in common locations
// Returns the path to Client.txt and whether it was found
func findPoEInstallation() (string, bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	
	// Common installation paths to check
	commonPaths := []string{
		// Standard standalone installation
		`C:\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`C:\Program Files\Grinding Gear Games\Path of Exile`,
		
		// Steam installations
		`C:\Program Files (x86)\Steam\steamapps\common\Path of Exile`,
		`C:\Program Files\Steam\steamapps\common\Path of Exile`,
		
		// Epic Games installation
		`C:\Program Files\Epic Games\Path of Exile`,
		`C:\Program Files (x86)\Epic Games\Path of Exile`,
	}
	
	// Add paths for other common drive letters (D:, E:, F:, G:)
	drives := []string{"D:", "E:", "F:", "G:"}
	basePaths := []string{
		`\Games\Path of Exile`,
		`\Path of Exile`,
		`\Program Files (x86)\Grinding Gear Games\Path of Exile`,
		`\Program Files\Grinding Gear Games\Path of Exile`,
		`\Steam\steamapps\common\Path of Exile`,
		`\Program Files (x86)\Steam\steamapps\common\Path of Exile`,
		`\Program Files\Steam\steamapps\common\Path of Exile`,
		`\Epic Games\Path of Exile`,
		`\Program Files\Epic Games\Path of Exile`,
		`\Program Files (x86)\Epic Games\Path of Exile`,
	}
	
	for _, drive := range drives {
		for _, basePath := range basePaths {
			commonPaths = append(commonPaths, drive+basePath)
		}
	}
	
	// Channel to receive results
	type result struct {
		path  string
		found bool
	}
	resultChan := make(chan result, len(commonPaths))
	doneChan := make(chan bool, 1)
	
	// Search paths concurrently with timeout
	for _, checkPath := range commonPaths {
		go func(p string) {
			if clientPath, found := validatePoEPath(p); found {
				select {
				case resultChan <- result{clientPath, true}:
				case <-doneChan:
				}
			}
		}(checkPath)
	}
	
	// Wait for first result or timeout
	timeout := time.After(8 * time.Second)
	select {
	case res := <-resultChan:
		close(doneChan) // Signal other goroutines to stop
		return res.path, res.found
	case <-timeout:
		close(doneChan)
		return "", false
	}
}

// getPoELogPath returns the path to Client.txt, searching if necessary
func getPoELogPath(config *Config) (string, error) {
	// If path is already configured, validate and use it
	if config.GamePath != "" {
		clientPath, valid := validatePoEPath(config.GamePath)
		if valid {
			return clientPath, nil
		}
		fmt.Printf("Configured path '%s' is no longer valid, searching for PoE installation...\n", config.GamePath)
	}
	
	// Search for PoE installation
	fmt.Println("Searching for Path of Exile installation...")
	clientPath, found := findPoEInstallation()
	
	if !found {
		return "", errors.New("could not find Path of Exile installation. Please set 'game_path' in your config file to the PoE installation directory")
	}
	
	// Extract the game directory from client path
	gameDir := filepath.Dir(filepath.Dir(clientPath)) // Remove \logs\Client.txt
	
	// Update config with found path
	config.GamePath = gameDir
	if err := saveConfig(config); err != nil {
		fmt.Printf("Warning: Could not save game path to config: %v\n", err)
	} else {
		fmt.Printf("Found and saved Path of Exile installation at: %s\n", gameDir)
	}
	
	return clientPath, nil
}

// saveConfig saves the current configuration to the config file
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
