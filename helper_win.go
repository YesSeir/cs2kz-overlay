//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func getLogFile(overridePath string) (*os.File, error) {
	if overridePath != "" {
		if _, err := os.Stat(overridePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("log file not found at override path: %s", overridePath)
		}
		file, err := os.Open(overridePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open override log file: %v", err)
		}
		return file, nil
	}

	root, err := getCS2Root()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(root, "game", "csgo", "console.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("log file not found at expected path: %s", logPath)
	}
	return os.Open(logPath)
}