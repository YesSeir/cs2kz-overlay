//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/andygrunwald/vdf"
	"golang.org/x/sys/windows/registry"
)

func getCS2Root() (string, error) {
	// Steam installation path
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.READ)
	if err != nil {
		return "", fmt.Errorf("Steam is not installed or the registry key was not found: %v", err)
	}
	defer key.Close()

	steamPath, _, err := key.GetStringValue("SteamPath")
	if err != nil {
		return "", fmt.Errorf("failed to read SteamPath: %v", err)
	}

	// libraryfolders.vdf path
	libraryfoldersPath := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
	if _, err := os.Stat(libraryfoldersPath); os.IsNotExist(err) {
		libraryfoldersPath = filepath.Join(steamPath, "config", "libraryfolders.vdf")
	}
	if _, err := os.Stat(libraryfoldersPath); os.IsNotExist(err) {
		return "", fmt.Errorf("libraryfolders.vdf not found at expected locations")
	}

	library, err := os.Open(libraryfoldersPath)
	if err != nil {
		return "", fmt.Errorf("failed to open libraryfolders.vdf: %v", err)
	}
	defer library.Close()

	parser := vdf.NewParser(library)
	vdfData, err := parser.Parse()
	if err != nil {
		return "", fmt.Errorf("failed to parse VDF: %v", err)
	}

	libraryfolders, ok := vdfData["libraryfolders"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("libraryfolders not found in VDF")
	}

	for _, folderValue := range libraryfolders {
		folder, ok := folderValue.(map[string]interface{})
		if !ok {
			continue
		}
		apps, ok := folder["apps"].(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasCS2 := apps["730"]; hasCS2 {
			path, ok := folder["path"].(string)
			if !ok {
				continue
			}
			manifestPath := filepath.Join(path, "steamapps", "appmanifest_730.acf")
			f, err := os.Open(manifestPath)
			if err != nil {
				return "", fmt.Errorf("failed to open appmanifest: %v", err)
			}
			defer f.Close()
			parser := vdf.NewParser(f)
			manifestData, err := parser.Parse()
			if err != nil {
				return "", fmt.Errorf("failed to parse appmanifest: %v", err)
			}
			appState, ok := manifestData["AppState"].(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("AppState not found in manifest")
			}
			installdir, ok := appState["installdir"].(string)
			if !ok {
				return "", fmt.Errorf("installdir not found in AppState")
			}
			return filepath.Join(path, "steamapps", "common", installdir), nil
		}
	}
	return "", fmt.Errorf("failed to find CS2 library path in any library folder")
}