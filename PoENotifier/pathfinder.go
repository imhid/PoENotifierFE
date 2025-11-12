package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	clientLogFileName = "Client.txt"
	pathOfExileExe    = "PathOfExile.exe"
)

func determineLogFilePath(config *Config, logger *log.Logger) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("log discovery is only supported on Windows")
	}

	// Priority order: config value -> environment override -> known locations -> user prompt
	if path, ok := ensureClientLogPath(config.LogPath); ok {
		logger.Printf("Using log path from configuration: %s", path)
		return path, nil
	} else if config.LogPath != "" {
		logger.Printf("Configured logPath did not point to a valid Client.txt: %s", config.LogPath)
	}

	if envPath, ok := ensureClientLogPath(os.Getenv("POE_LOG_PATH")); ok {
		logger.Printf("Using log path from environment variable POE_LOG_PATH: %s", envPath)
		return envPath, nil
	}

	if envInstall := os.Getenv("POE_INSTALL_PATH"); envInstall != "" {
		if derived, ok := ensureClientLogPath(filepath.Join(envInstall, "logs", clientLogFileName)); ok {
			logger.Printf("Using log path derived from POE_INSTALL_PATH: %s", derived)
			return derived, nil
		}
	}

	for _, candidate := range knownLogLocations() {
		if path, ok := ensureClientLogPath(candidate); ok {
			logger.Printf("Automatically discovered log file at: %s", path)
			return path, nil
		}
	}

	logger.Println("Automatic discovery failed, prompting user for Path of Exile installation or log path.")
	userPath, err := promptForLogPath()
	if err != nil {
		return "", err
	}

	if resolved, ok := ensureClientLogPath(userPath); ok {
		logger.Printf("Using user-provided log path: %s", resolved)
		config.LogPath = resolved
		if err := saveConfig(config); err != nil {
			logger.Printf("Failed to persist log path to config: %v", err)
		}
		return resolved, nil
	}

	return "", fmt.Errorf("unable to validate user supplied path: %s", userPath)
}

func knownLogLocations() []string {
	locations := make([]string, 0, 64)
	seen := map[string]struct{}{}

	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		locations = append(locations, path)
	}

	if home, err := os.UserHomeDir(); err == nil {
		docPaths := []string{
			filepath.Join(home, "Documents", "My Games", "Path of Exile", "Logs", clientLogFileName),
			filepath.Join(home, "Documents", "My Games", "Path of Exile", clientLogFileName),
			filepath.Join(home, "OneDrive", "Documents", "My Games", "Path of Exile", "Logs", clientLogFileName),
			filepath.Join(home, "OneDrive", "Documents", "My Games", "Path of Exile", clientLogFileName),
		}
		for _, docPath := range docPaths {
			add(docPath)
		}
	}

	systemRoots := []string{"C"}
	for letter := 'D'; letter <= 'Z'; letter++ {
		systemRoots = append(systemRoots, string(letter))
	}

	commonSubdirs := [][]string{
		{"Program Files", "Grinding Gear Games", "Path of Exile"},
		{"Program Files (x86)", "Grinding Gear Games", "Path of Exile"},
		{"Program Files", "Steam", "steamapps", "common", "Path of Exile"},
		{"Program Files (x86)", "Steam", "steamapps", "common", "Path of Exile"},
		{"SteamLibrary", "steamapps", "common", "Path of Exile"},
		{"Games", "Path of Exile"},
		{"Games", "Grinding Gear Games", "Path of Exile"},
		{"Path of Exile"},
	}

	for _, root := range systemRoots {
		drivePath := fmt.Sprintf("%s:\\", root)
		for _, sub := range commonSubdirs {
			base := filepath.Join(append([]string{drivePath}, sub...)...)
			add(filepath.Join(base, "logs", clientLogFileName))
			add(filepath.Join(base, clientLogFileName))
		}
	}

	return locations
}

func ensureClientLogPath(input string) (string, bool) {
	clean := normalizePath(input)
	if clean == "" {
		return "", false
	}

	if info, err := os.Stat(clean); err == nil && !info.IsDir() {
		if strings.EqualFold(filepath.Base(clean), clientLogFileName) {
			return clean, true
		}

		if strings.EqualFold(filepath.Base(clean), pathOfExileExe) {
			return derivedLogFromInstall(filepath.Dir(clean))
		}
	}

	if info, err := os.Stat(clean); err == nil && info.IsDir() {
		if resolved, ok := derivedLogFromInstall(clean); ok {
			return resolved, true
		}
	}

	return "", false
}

func derivedLogFromInstall(installDir string) (string, bool) {
	candidates := []string{
		filepath.Join(installDir, "logs", clientLogFileName),
		filepath.Join(installDir, "Logs", clientLogFileName),
		filepath.Join(installDir, clientLogFileName),
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}

	return "", false
}

func promptForLogPath() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	for attempts := 0; attempts < 3; attempts++ {
		fmt.Print("Enter the Path of Exile installation directory, PathOfExile.exe location, or Client.txt path: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return "", fmt.Errorf("input closed while waiting for path: %w", err)
			}
			return "", err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if resolved, ok := ensureClientLogPath(input); ok {
			return resolved, nil
		}
		fmt.Printf("Client.txt could not be found based on '%s'. Please try again.\n", input)
	}
	return "", errors.New("failed to locate Client.txt after 3 attempts")
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "\"'")
	if p == "" {
		return ""
	}
	p = filepath.FromSlash(p)
	return filepath.Clean(p)
}
