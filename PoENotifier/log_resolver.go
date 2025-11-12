package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	logFileName              = "Client.txt"
	defaultLogsRelativePath  = "logs"
	autoDetectionDeadline    = 8 * time.Second
	maxInteractiveAttempts   = 3
	steamCommonRelativePath  = "Steam\\steamapps\\common\\Path of Exile"
	steamLibraryRelativePath = "SteamLibrary\\steamapps\\common\\Path of Exile"
)

func resolveLogPath(config *Config, logger *log.Logger) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("log path resolution is currently supported only on Windows")
	}

	if path, ok := ensureValidLogPath(config.LogPath); ok {
		logger.Printf("Using log path from configuration: %s", path)
		return path, nil
	}

	if path, err := autoDetectLogPath(logger); err == nil {
		logger.Printf("Automatically detected log path: %s", path)
		if config.LogPath != path {
			config.LogPath = path
			if err := saveConfig(config); err != nil {
				logger.Printf("Failed to persist detected log path: %v", err)
			}
		}
		return path, nil
	} else {
		logger.Printf("Automatic log detection failed: %v", err)
	}

	if !isInteractiveSession() {
		message := "Unable to find Path of Exile log automatically. Please update `log_path` in the notifier configuration."
		logger.Println(message)
		_ = showToast("PoE Notifier", "Configure `log_path` in notifier_config.json")
		return "", errors.New("log path could not be resolved automatically and interactive prompt is unavailable")
	}

	if path, err := promptUserForLogPath(); err == nil {
		if resolved, ok := ensureValidLogPath(path); ok {
			logger.Printf("Using user supplied log path: %s", resolved)
			config.LogPath = resolved
			if err := saveConfig(config); err != nil {
				logger.Printf("Failed to save user supplied log path: %v", err)
			}
			return resolved, nil
		}
		logger.Printf("Provided path does not contain %s: %s", logFileName, path)
	} else {
		return "", err
	}

	return "", errors.New("log path could not be determined")
}

func autoDetectLogPath(logger *log.Logger) (string, error) {
	deadline := time.Now().Add(autoDetectionDeadline)

	for _, candidate := range knownLogCandidates() {
		if resolved, ok := ensureValidLogPath(candidate); ok {
			return resolved, nil
		}
	}

	for _, drive := range detectedDrives() {
		if time.Now().After(deadline) {
			break
		}

		if path, found := searchDriveForLog(drive, deadline); found {
			return path, nil
		}
	}

	return "", errors.New("log file not found in known locations")
}

func ensureValidLogPath(candidate string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", false
	}

	candidate = strings.Trim(candidate, `"`)
	candidate = os.ExpandEnv(candidate)
	cleaned := filepath.Clean(candidate)

	possible := orderedUniquePaths(
		cleaned,
		filepath.Join(cleaned, logFileName),
		filepath.Join(cleaned, defaultLogsRelativePath, logFileName),
		filepath.Join(filepath.Dir(cleaned), defaultLogsRelativePath, logFileName),
	)

	for _, path := range possible {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Base(path), logFileName) {
			return path, true
		}
	}

	return "", false
}

func knownLogCandidates() []string {
	var candidates []string
	user, err := user.Current()
	if err == nil {
		candidates = append(candidates,
			filepath.Join(user.HomeDir, "Documents", "My Games", "Path of Exile", "Logs", logFileName),
		)
	}

	programFiles := []string{
		os.Getenv("PROGRAMFILES"),
		os.Getenv("PROGRAMFILES(X86)"),
		os.Getenv("ProgramW6432"),
		`C:\Program Files`,
		`C:\Program Files (x86)`,
	}

	for _, base := range orderedUniquePaths(programFiles...) {
		candidates = append(candidates,
			filepath.Join(base, "Grinding Gear Games", "Path of Exile", defaultLogsRelativePath, logFileName),
			filepath.Join(base, "Steam", "steamapps", "common", "Path of Exile", defaultLogsRelativePath, logFileName),
			filepath.Join(base, "Steam (x86)", "steamapps", "common", "Path of Exile", defaultLogsRelativePath, logFileName),
		)
	}

	for _, drive := range detectedDrives() {
		candidates = append(candidates,
			filepath.Join(drive, "Path of Exile", defaultLogsRelativePath, logFileName),
			filepath.Join(drive, "Games", "Path of Exile", defaultLogsRelativePath, logFileName),
			filepath.Join(drive, "Grinding Gear Games", "Path of Exile", defaultLogsRelativePath, logFileName),
			filepath.Join(drive, "Steam", "steamapps", "common", "Path of Exile", defaultLogsRelativePath, logFileName),
			filepath.Join(drive, "SteamLibrary", "steamapps", "common", "Path of Exile", defaultLogsRelativePath, logFileName),
		)
	}

	return orderedUniquePaths(candidates...)
}

func detectedDrives() []string {
	if runtime.GOOS != "windows" {
		return nil
	}

	var drives []string
	for letter := 'C'; letter <= 'Z'; letter++ {
		drive := fmt.Sprintf("%c:\\", letter)
		if _, err := os.Stat(drive); err == nil {
			drives = append(drives, drive)
		}
	}
	return drives
}

func searchDriveForLog(drive string, deadline time.Time) (string, bool) {
	targets := []string{
		filepath.Join(drive, steamCommonRelativePath, defaultLogsRelativePath, logFileName),
		filepath.Join(drive, steamLibraryRelativePath, defaultLogsRelativePath, logFileName),
	}

	for _, target := range orderedUniquePaths(targets...) {
		if time.Now().After(deadline) {
			return "", false
		}
		if resolved, ok := ensureValidLogPath(target); ok {
			return resolved, true
		}
	}

	commonRoots := []string{
		filepath.Join(drive, "Games"),
		filepath.Join(drive, "SteamLibrary"),
		filepath.Join(drive, "Program Files"),
		filepath.Join(drive, "Program Files (x86)"),
		filepath.Join(drive, "Grinding Gear Games"),
	}

	for _, root := range orderedUniquePaths(commonRoots...) {
		if time.Now().After(deadline) {
			return "", false
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		if path, found := scanForPathOfExile(root, deadline); found {
			return path, true
		}
	}

	return "", false
}

func scanForPathOfExile(root string, deadline time.Time) (string, bool) {
	const maxDepth = 3
	type item struct {
		path  string
		depth int
	}

	queue := []item{{path: root, depth: 0}}

	for len(queue) > 0 {
		if time.Now().After(deadline) {
			return "", false
		}

		current := queue[0]
		queue = queue[1:]

		dirEntries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}

		for _, entry := range dirEntries {
			if entry.IsDir() {
				nextPath := filepath.Join(current.path, entry.Name())
				if strings.Contains(strings.ToLower(entry.Name()), "path of exile") {
					if resolved, ok := ensureValidLogPath(nextPath); ok {
						return resolved, true
					}
				}
				if current.depth+1 <= maxDepth {
					queue = append(queue, item{path: nextPath, depth: current.depth + 1})
				}
			}
		}
	}

	return "", false
}

func promptUserForLogPath() (string, error) {
	reader := bufio.NewReader(os.Stdin)

	for attempt := 0; attempt < maxInteractiveAttempts; attempt++ {
		fmt.Printf("Enter the full path to Path of Exile's %s (attempt %d of %d):\n> ", logFileName, attempt+1, maxInteractiveAttempts)
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if resolved, ok := ensureValidLogPath(input); ok {
			return resolved, nil
		}

		fmt.Printf("Could not find %s at the provided path. Please try again.\n", logFileName)
	}

	return "", errors.New("maximum attempts reached without a valid log path")
}

func isInteractiveSession() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func orderedUniquePaths(paths ...string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		cleaned := filepath.Clean(p)
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}
