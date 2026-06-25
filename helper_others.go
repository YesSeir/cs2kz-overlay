//go:build !windows

package main

import (
	"fmt"
	"os"
)

func getLogFile(path string) (*os.File, error) {
	if path != "" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("log file not found at override path: %s", path)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open override log file: %v", err)
		}
		return file, nil
	}
	return nil, fmt.Errorf("-log-file is required on non-Windows platforms")
}
