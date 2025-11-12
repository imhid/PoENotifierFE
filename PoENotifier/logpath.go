package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	logDetectionTimeBudget = 8 * time.Second
	userInputTimeout       = 120 * time.Second
)

func resolveLogFilePath(cfg *Config, logger *log.Logger) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("log file detection only supports Windows")
	}

	type candidate struct {
		path   string
		source string
	}

	seen := make(map[string]struct{})
	var candidates []candidate
	addCandidate := func(raw, source string) {
		normalized := normalizeCandidatePath(raw)
		if normalized == "" {
			return
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, candidate{path: normalized, source: source})
	}

	if cfg != nil && cfg.LogFilePath != "" {
		addCandidate(cfg.LogFilePath, "logFilePath from config")
	}

	if cfg != nil {
		for _, installPath := range cfg.InstallPaths {
			clean := normalizeCandidatePath(installPath)
			if clean == "" {
				continue
			}

			if strings.EqualFold(filepath.Base(clean), "Client.txt") {
				addCandidate(clean, "installPaths entry (file)")
				continue
			}

			addCandidate(filepath.Join(clean, "logs", "Client.txt"), "installPaths entry + logs")
			addCandidate(filepath.Join(clean, "Client.txt"), "installPaths entry")
		}
	}

	for _, candidate := range defaultLogFileCandidates() {
		addCandidate(candidate, "built-in default path")
	}

	deadline := time.Now().Add(logDetectionTimeBudget)
	for _, candidate := range candidates {
		if time.Now().After(deadline) {
			logger.Printf("Stopping automatic Path of Exile log search after %s", logDetectionTimeBudget)
			break
		}
		if resolved := resolveCandidate(candidate.path); resolved != "" {
			logger.Printf("Using Path of Exile log file at %s (%s)", resolved, candidate.source)
			if cfg != nil && cfg.LogFilePath == "" {
				cfg.LogFilePath = resolved
			}
			return resolved, nil
		}
	}

	logger.Println("Unable to locate Path of Exile Client.txt automatically.")
	return requestLogFileFromUser(cfg, logger)
}

func normalizeCandidatePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = filepath.FromSlash(trimmed)
	return filepath.Clean(trimmed)
}

func resolveCandidate(candidate string) string {
	if candidate == "" {
		return ""
	}

	info, err := os.Stat(candidate)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		for _, next := range []string{
			filepath.Join(candidate, "Client.txt"),
			filepath.Join(candidate, "logs", "Client.txt"),
		} {
			if resolved := resolveCandidate(next); resolved != "" {
				return resolved
			}
		}
		return ""
	}

	name := strings.ToLower(info.Name())
	if name == "client.txt" {
		return candidate
	}

	switch name {
	case "pathofexile.exe", "pathofexilesteam.exe", "pathofexile_x64.exe":
		logCandidate := filepath.Join(filepath.Dir(candidate), "logs", "Client.txt")
		return resolveCandidate(logCandidate)
	}

	return ""
}

func defaultLogFileCandidates() []string {
	drives := []string{"C:", "D:", "E:", "F:"}
	suffixes := []string{
		`\Program Files (x86)\Grinding Gear Games\Path of Exile\logs\Client.txt`,
		`\Program Files\Grinding Gear Games\Path of Exile\logs\Client.txt`,
		`\Program Files (x86)\Steam\steamapps\common\Path of Exile\logs\Client.txt`,
		`\Program Files\Steam\steamapps\common\Path of Exile\logs\Client.txt`,
		`\SteamLibrary\steamapps\common\Path of Exile\logs\Client.txt`,
		`\Games\Path of Exile\logs\Client.txt`,
		`\Path of Exile\logs\Client.txt`,
	}

	var candidates []string
	for _, drive := range drives {
		for _, suffix := range suffixes {
			candidates = append(candidates, drive+suffix)
		}
	}

	if pf := os.Getenv("PROGRAMFILES"); pf != "" {
		candidates = append(candidates,
			filepath.Join(pf, "Grinding Gear Games", "Path of Exile", "logs", "Client.txt"),
			filepath.Join(pf, "Steam", "steamapps", "common", "Path of Exile", "logs", "Client.txt"),
		)
	}

	if pf := os.Getenv("PROGRAMFILES(X86)"); pf != "" {
		candidates = append(candidates,
			filepath.Join(pf, "Grinding Gear Games", "Path of Exile", "logs", "Client.txt"),
			filepath.Join(pf, "Steam", "steamapps", "common", "Path of Exile", "logs", "Client.txt"),
		)
	}

	return candidates
}

func requestLogFileFromUser(cfg *Config, logger *log.Logger) (string, error) {
	if !isInteractiveTerminal() {
		return "", errors.New("automatic log detection failed. Please set 'logFilePath' in the notifier configuration file")
	}

	fmt.Println("Path of Exile log file not found automatically.")
	fmt.Println("Please enter the full path to Client.txt (e.g. D:\\Games\\Path of Exile\\logs\\Client.txt):")

	reader := bufio.NewReader(os.Stdin)
	providedPath, err := readLineWithTimeout(reader, userInputTimeout)
	if err != nil {
		return "", err
	}

	providedPath = normalizeCandidatePath(providedPath)
	if providedPath == "" {
		return "", errors.New("no path provided")
	}

	resolved := resolveCandidate(providedPath)
	if resolved == "" {
		return "", fmt.Errorf("the provided path '%s' does not appear to contain Client.txt", providedPath)
	}

	logger.Printf("Using Path of Exile log file provided by user: %s", resolved)

	if cfg != nil {
		cfg.LogFilePath = resolved
		if err := saveConfig(cfg); err != nil {
			logger.Printf("Failed to persist logFilePath to configuration: %v", err)
		}
	}

	return resolved, nil
}

func readLineWithTimeout(reader *bufio.Reader, timeout time.Duration) (string, error) {
	inputCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			errCh <- err
			return
		}
		inputCh <- strings.TrimSpace(line)
	}()

	if timeout <= 0 {
		timeout = userInputTimeout
	}

	select {
	case line := <-inputCh:
		return line, nil
	case err := <-errCh:
		return "", err
	case <-time.After(timeout):
		return "", errors.New("timed out waiting for user input")
	}
}

func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
