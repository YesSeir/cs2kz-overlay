package main

import (
	"fmt"
	"regexp"
)


func parseAuthStatus(line string) bool {
	if gameState.PlayerID != 0 {
		return false
	}
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[SteamNetSockets\] AuthStatus \(steamid:(\d+)\)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 2 {
		return false
	}
	var steamID uint64
	_, err := fmt.Sscanf(matches[1], "%d", &steamID)
	if err != nil {
		return false
	}
	gameState.PlayerID = steamID
	return true
}

// 06/15 08:18:48 [Client] Map: "kz_labour"
func parseMapStart(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[Client\] Map: "([^"]+)"`)
	matches := re.FindStringSubmatch(line)

	if len(matches) < 2 {
		return false
	}
	// Reset map/course/zone info on new map start
	gameState.MapName = matches[1]
	gameState.CourseName = ""
	gameState.Splits = nil
	gameState.Checkpoints = nil
	gameState.Stages = nil
	return true
}

// 06/15 08:18:48 [CS2KZ] timer_start|main|5|2|1|CKZ
func parseTimerStart(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[CS2KZ\] timer_start\|([^|]+)\|(\d+)\|(\d+)\|(\d+)\|([^\r\n]*)`)
	matches := re.FindStringSubmatch(line)

	if len(matches) < 6 {
		return false
	}
	// Course "main", 5 splits, 2 checkpoints, 1 stage, mode CKZ
	gameState.CourseName = matches[1]
	gameState.PlayerMode = matches[5]

	var splits, checkpoints, stages int
	fmt.Sscanf(matches[2], "%d", &splits)
	fmt.Sscanf(matches[3], "%d", &checkpoints)
	fmt.Sscanf(matches[4], "%d", &stages)
	gameState.Splits = make([]float32, splits)
	gameState.Checkpoints = make([]float32, checkpoints)
	gameState.Stages = make([]float32, stages)
	gameState.TimerState = "running"
	return true
}

func parseTimerStop(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[CS2KZ\] timer_stop`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 1 {
		return false
	}
	if gameState.TimerState != "running" {
		if gameState.TimerState != "" {
			fmt.Printf("Warning: timer_stop received but timer was not running (current state: %s)\n", gameState.TimerState)
		}
		return false
	}
	gameState.TimerState = "stopped"
	return true
}

func parseTimerEnd(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[CS2KZ\] timer_end`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 1 {
		return false
	}
	if gameState.TimerState != "running" {
		if gameState.TimerState != "" {
			fmt.Printf("Warning: timer_end received but timer was not running (current state: %s)\n", gameState.TimerState)
		}
		return false
	}
	gameState.TimerState = "finished"
	return true
}

// 06/15 08:18:48 [CS2KZ] split|1|12.345
func parseSplit(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[CS2KZ\] split\|(\d+)\|([\d.]+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 {
		return false
	}
	var splitNum int
	var splitTime float64
	fmt.Sscanf(matches[1], "%d", &splitNum)
	fmt.Sscanf(matches[2], "%f", &splitTime)
	gameState.Splits[splitNum-1] = float32(splitTime)
	return true
}

func parseCheckpoint(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[CS2KZ\] checkpoint\|(\d+)\|([\d.]+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 {
		return false
	}
	var checkpointNum int
	var checkpointTime float64
	fmt.Sscanf(matches[1], "%d", &checkpointNum)
	fmt.Sscanf(matches[2], "%f", &checkpointTime)
	gameState.Checkpoints[checkpointNum-1] = float32(checkpointTime)
	return true
}

func parseStage(line string) bool {
	var re = regexp.MustCompile(`^\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[CS2KZ\] stage\|(\d+)\|([\d.]+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 {
		return false
	}
	var stageNum int
	var stageTime float64
	fmt.Sscanf(matches[1], "%d", &stageNum)
	fmt.Sscanf(matches[2], "%f", &stageTime)
	gameState.Stages[stageNum-1] = float32(stageTime)
	return true
}

// Return true if the line was successfully parsed as a game event, false otherwise
func parseLogLine(line string) bool {
	if parseAuthStatus(line) {
		return true
	}

	if parseMapStart(line) {
		return true
	}

	if parseTimerStart(line) {
		return true
	}

	if parseTimerStop(line) {
		return true
	}

	if parseTimerEnd(line) {
		return true
	}

	if parseSplit(line) {
		return true
	}

	if parseCheckpoint(line) {
		return true
	}

	if parseStage(line) {
		return true
	}
	return false
}
