package main

import (
	_ "embed"
	"bufio"
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"github.com/getlantern/systray"
)

//go:embed icon.ico
var iconData []byte

//go:embed web
var webFS embed.FS

//go:embed web/progress.html
var progressHTML string

type GameState struct {
	MapName     string    `json:"map"`
	CourseName  string    `json:"course"`
	PlayerID    uint64    `json:"steamID,string"`
	PlayerMode  string    `json:"mode"`
	Splits      []float32 `json:"splits"`
	Checkpoints []float32 `json:"checkpoints"`
	Stages      []float32 `json:"stages"`
	TimerState  string    `json:"timer"`
}

var (
	clients   = make(map[chan string]bool)
	clientsMu sync.RWMutex
	gameState GameState
)

var (
	serverMapsCache  []map[string]interface{}
	serverMapsMu     sync.RWMutex
	serverMapsLoaded bool
)

func loadServerMaps() error {
	serverMapsMu.Lock()
	defer serverMapsMu.Unlock()
	if serverMapsLoaded {
		return nil
	}
	resp, err := http.Get("https://raw.githubusercontent.com/YesSeir/cs2kz-maps/main/maps.json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var data []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	serverMapsCache = data
	serverMapsLoaded = true
	return nil
}

type globalCourseEntry struct {
	NubTier int
	ProTier int
	Ranked  bool
}

var (
	globalApprovedCache     map[string]globalCourseEntry
	globalMainCourseNameMap map[string]string
	globalCacheMu           sync.RWMutex
	globalCacheLoaded       bool
)

var tierNameToNumber = map[string]int{
	"very-easy": 1, "easy": 2, "medium": 3, "advanced": 4,
	"hard": 5, "very-hard": 6, "extreme": 7, "death": 8,
	"unfeasible": 9, "impossible": 10,
}

type APIMapResponse struct {
	Total  int `json:"total"`
	Values []struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Courses  []struct {
			Name    string `json:"name"`
			Filters struct {
				Vanilla struct {
					NubTier string `json:"nub_tier"`
					ProTier string `json:"pro_tier"`
					State   string `json:"state"`
				} `json:"vanilla"`
				Classic struct {
					NubTier string `json:"nub_tier"`
					ProTier string `json:"pro_tier"`
					State   string `json:"state"`
				} `json:"classic"`
			} `json:"filters"`
		} `json:"courses"`
	} `json:"values"`
}

type APIRecord struct {
	Player struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"player"`
	Map struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"map"`
	Course struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		NubTier string `json:"nub_tier"`
		ProTier string `json:"pro_tier"`
		State   string `json:"state"`
	} `json:"course"`
	Mode      string  `json:"mode"`
	Teleports int     `json:"teleports"`
	Time      float64 `json:"time"`
}

type APIRecordsResponse struct {
	Total  int         `json:"total"`
	Values []APIRecord `json:"values"`
}

func loadGlobalApprovedMaps() error {
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	if globalCacheLoaded {
		return nil
	}

	resp, err := http.Get("https://api.cs2kz.org/maps")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var apiResp APIMapResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}

	cache := make(map[string]globalCourseEntry)
	mainCourseMap := make(map[string]string)

	for _, m := range apiResp.Values {
		if m.State != "approved" {
			continue
		}
		if len(m.Courses) == 0 {
			continue
		}

		mainCourseName := m.Courses[0].Name
		mainCourseMap[strings.ToLower(m.Name)] = strings.ToLower(mainCourseName)

		for _, c := range m.Courses {
			keyClassic := fmt.Sprintf("%s|%s|classic", strings.ToLower(m.Name), strings.ToLower(c.Name))
			nubClassic := tierNameToNumber[c.Filters.Classic.NubTier]
			proClassic := tierNameToNumber[c.Filters.Classic.ProTier]
			rankedClassic := c.Filters.Classic.State == "ranked"
			cache[keyClassic] = globalCourseEntry{
				NubTier: nubClassic,
				ProTier: proClassic,
				Ranked:  rankedClassic,
			}
			keyVanilla := fmt.Sprintf("%s|%s|vanilla", strings.ToLower(m.Name), strings.ToLower(c.Name))
			nubVanilla := tierNameToNumber[c.Filters.Vanilla.NubTier]
			proVanilla := tierNameToNumber[c.Filters.Vanilla.ProTier]
			rankedVanilla := c.Filters.Vanilla.State == "ranked"
			cache[keyVanilla] = globalCourseEntry{
				NubTier: nubVanilla,
				ProTier: proVanilla,
				Ranked:  rankedVanilla,
			}
		}
	}
	globalApprovedCache = cache
	globalMainCourseNameMap = mainCourseMap
	globalCacheLoaded = true
	return nil
}

func steamID64ToSteamID(id uint64) string {
	accountID := id & 0xFFFFFFFF
	authServer := (id >> 32) & 1
	return fmt.Sprintf("STEAM_1:%d:%d", authServer, accountID/2)
}

func fetchPlayerRecords(steamID uint64, mode string, hasTeleports *bool) ([]APIRecord, error) {
	steamStr := steamID64ToSteamID(steamID)
	url := fmt.Sprintf("https://api.cs2kz.org/records?player=%s&mode=%s&top=true&ranked=true", steamStr, mode)
	if hasTeleports != nil {
		if *hasTeleports {
			url += "&has_teleports=true"
		} else {
			url += "&has_teleports=false"
		}
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result APIRecordsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Values, nil
}

func apiProgressHandler(w http.ResponseWriter, r *http.Request) {
	typeParam := r.URL.Query().Get("type")
	courseParam := r.URL.Query().Get("course")
	globalParam := r.URL.Query().Get("global")

	if typeParam != "all" && typeParam != "pro" {
		http.Error(w, "type must be 'all' or 'pro'", http.StatusBadRequest)
		return
	}
	if courseParam != "main" && courseParam != "bonus" && courseParam != "all" {
		courseParam = "all"
	}

	mode := gameState.PlayerMode
	if mode == "" {
		mode = "CKZ"
	}
	var modeStr string
	if mode == "CKZ" {
		modeStr = "classic"
	} else {
		modeStr = "vanilla"
	}
	isPro := typeParam == "pro"

	if globalParam == "false" || globalParam == "0" {
		if err := loadGlobalApprovedMaps(); err != nil {
			http.Error(w, "Failed to load global maps: "+err.Error(), http.StatusInternalServerError)
			return
		}

		steamID := gameState.PlayerID
		if steamID == 0 {
			result := make(map[string]map[string]int)
			for i := 1; i <= 8; i++ {
				result[fmt.Sprintf("%d", i)] = map[string]int{"total": 0, "completed": 0}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
			return
		}

		var records []APIRecord
		var err error
		if isPro {
			hasTeleportsFalse := false
			records, err = fetchPlayerRecords(steamID, modeStr, &hasTeleportsFalse)
		} else {
			records, err = fetchPlayerRecords(steamID, modeStr, nil)
		}
		if err != nil {
			log.Printf("[ERROR] Failed to fetch player records: %v", err)
			result := make(map[string]map[string]int)
			for i := 1; i <= 8; i++ {
				result[fmt.Sprintf("%d", i)] = map[string]int{"total": 0, "completed": 0}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
			return
		}

		completedMap := make(map[string]bool)
		for _, rec := range records {
			key := strings.ToLower(rec.Map.Name) + "|" + strings.ToLower(rec.Course.Name)
			completedMap[key] = true
		}

		globalCacheMu.RLock()
		cache := globalApprovedCache
		mainMap := globalMainCourseNameMap
		globalCacheMu.RUnlock()

		filteredCache := make(map[string]globalCourseEntry)
		for key, entry := range cache {
			parts := strings.Split(key, "|")
			if len(parts) != 3 {
				continue
			}
			mapName := parts[0]
			courseName := parts[1]
			modeKey := parts[2]

			if modeKey != modeStr {
				continue
			}
			if !entry.Ranked {
				continue
			}

			isMain := false
			if mainName, ok := mainMap[mapName]; ok {
				if courseName == mainName {
					isMain = true
				}
			} else {
				continue
			}

			if courseParam == "main" && !isMain {
				continue
			}
			if courseParam == "bonus" && isMain {
				continue
			}

			filteredCache[key] = entry
		}

		totalTiers := make(map[int]int)
		completedTiers := make(map[int]int)
		for i := 1; i <= 8; i++ {
			totalTiers[i] = 0
			completedTiers[i] = 0
		}

		for key, entry := range filteredCache {
			parts := strings.Split(key, "|")
			mapName := parts[0]
			courseName := parts[1]

			tier := entry.NubTier
			if isPro {
				tier = entry.ProTier
			}
			if tier < 1 || tier > 8 {
				continue
			}

			totalTiers[tier]++
			pairKey := mapName + "|" + courseName
			if completedMap[pairKey] {
				completedTiers[tier]++
			}
		}

		result := make(map[string]map[string]int)
		for i := 1; i <= 8; i++ {
			result[fmt.Sprintf("%d", i)] = map[string]int{
				"total":     totalTiers[i],
				"completed": completedTiers[i],
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	// global=false
	if err := loadServerMaps(); err != nil {
		http.Error(w, "Failed to load maps.json: "+err.Error(), http.StatusInternalServerError)
		return
	}
	serverMapsMu.RLock()
	maps := serverMapsCache
	serverMapsMu.RUnlock()

	var tierField string
	if mode == "CKZ" {
		if isPro {
			tierField = "ckzprotier"
		} else {
			tierField = "ckznubtier"
		}
	} else {
		if isPro {
			tierField = "vnlprotier"
		} else {
			tierField = "vnlnubtier"
		}
	}

	type CourseInfo struct {
		CourseID int
		Tier     int
	}
	courseInfoMap := make(map[string]CourseInfo)
	for _, entry := range maps {
		mapName, ok1 := entry["mapname"].(string)
		courseName, ok2 := entry["coursename"].(string)
		courseid, ok3 := entry["courseid"].(float64)
		tierVal, ok4 := entry[tierField].(float64)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		tier := int(tierVal)
		if tier < 1 || tier > 8 {
			continue
		}
		key := strings.ToLower(mapName) + "|" + strings.ToLower(courseName)
		courseInfoMap[key] = CourseInfo{
			CourseID: int(courseid),
			Tier:     tier,
		}
	}

	totalTiers := make(map[int]int)
	for i := 1; i <= 8; i++ {
		totalTiers[i] = 0
	}
	for _, info := range courseInfoMap {
		if courseParam == "main" && info.CourseID != 1 {
			continue
		}
		if courseParam == "bonus" && info.CourseID <= 1 {
			continue
		}
		totalTiers[info.Tier]++
	}

	root, err := getCS2Root()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dbPath := filepath.Join(root, "game", "csgo", "addons", "cs2kz", "data", "cs2kz.sqlite3")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		http.Error(w, "SQLite database not found", http.StatusNotFound)
		return
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var modeID int
	if mode == "CKZ" {
		modeID = 2
	} else {
		modeID = 1
	}

	teleportsCondition := ""
	if isPro {
		teleportsCondition = "AND t.Teleports = 0"
	}

	completedQuery := fmt.Sprintf(`
		SELECT DISTINCT m.Name, mc.Name
		FROM Times t
		JOIN MapCourses mc ON t.MapCourseID = mc.ID
		JOIN Maps m ON mc.MapID = m.ID
		WHERE t.ModeID = ? AND t.RunTime > 0 %s
	`, teleportsCondition)

	rows, err := db.Query(completedQuery, modeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	completedTiers := make(map[int]int)
	for i := 1; i <= 8; i++ {
		completedTiers[i] = 0
	}
	for rows.Next() {
		var mapName, courseName string
		if err := rows.Scan(&mapName, &courseName); err != nil {
			continue
		}
		key := strings.ToLower(mapName) + "|" + strings.ToLower(courseName)
		info, ok := courseInfoMap[key]
		if !ok {
			continue
		}
		if courseParam == "main" && info.CourseID != 1 {
			continue
		}
		if courseParam == "bonus" && info.CourseID <= 1 {
			continue
		}
		completedTiers[info.Tier]++
	}

	result := make(map[string]map[string]int)
	for i := 1; i <= 8; i++ {
		result[fmt.Sprintf("%d", i)] = map[string]int{
			"total":     totalTiers[i],
			"completed": completedTiers[i],
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func progressPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, progressHTML)
}

func broadcast(message string) {
	fmt.Printf("Broadcasting message: %s\n", message)
	clientsMu.RLock()
	for client := range clients {
		select {
		case client <- message:
		default:
		}
	}
	clientsMu.RUnlock()
}

func localWRHandler(w http.ResponseWriter, r *http.Request) {
	mapName := r.URL.Query().Get("map")
	courseName := r.URL.Query().Get("course")
	mode := r.URL.Query().Get("mode")
	teleports := r.URL.Query().Get("teleports")

	if mapName == "" || courseName == "" || mode == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	modeID := 2
	if mode == "vanilla" {
		modeID = 1
	}

	root, err := getCS2Root()
	if err != nil {
		log.Printf("[localWR] getCS2Root error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dbPath := filepath.Join(root, "game", "csgo", "addons", "cs2kz", "data", "cs2kz.sqlite3")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Printf("[localWR] SQLite not found at: %s", dbPath)
		http.Error(w, "SQLite database not found", http.StatusNotFound)
		return
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("[localWR] sql.Open error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	query := `
		SELECT t.RunTime, p.Alias, t.SteamID64
		FROM Times t
		JOIN MapCourses mc ON t.MapCourseID = mc.ID
		JOIN Maps m ON mc.MapID = m.ID
		JOIN Players p ON t.SteamID64 = p.SteamID64
		WHERE LOWER(m.Name) = LOWER(?)
		  AND LOWER(mc.Name) = LOWER(?)
		  AND t.ModeID = ?
		  AND t.RunTime > 0
	`
	args := []interface{}{mapName, courseName, modeID}

	if teleports == "0" {
		query += " AND t.Teleports = 0"
	} else if teleports == "1" {
		query += " AND t.Teleports = 1"
	}

	query += " ORDER BY t.RunTime ASC LIMIT 1"

	var runTime float64
	var playerName, steamID string
	err = db.QueryRow(query, args...).Scan(&runTime, &playerName, &steamID)
	if err == sql.ErrNoRows {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"found": false})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"found":      true,
		"time":       runTime,
		"playerName": playerName,
		"steamId":    steamID,
	})
}

func watchLog(logFilePath string) {
	var file *os.File
	var reader *bufio.Reader
	var shouldBroadcast bool

	for {
		if file == nil {
			var err error
			file, err = getLogFile(logFilePath)
			if err != nil {
				log.Printf("failed to open log file: %v", err)
				time.Sleep(time.Second)
				continue
			}
			reader = bufio.NewReader(file)
		}

		line, err := reader.ReadString('\n')

		if err == nil {
			if parseLogLine(line) {
				shouldBroadcast = true
			}
			continue
		}
		if err == io.EOF {
			if line != "" {
				if parseLogLine(line) {
					shouldBroadcast = true
				}
			}

			if shouldBroadcast {
				if jsonData, err := JSONMarshal(gameState); err == nil {
					broadcast(string(jsonData))
				}
				shouldBroadcast = false
			}

			time.Sleep(20 * time.Millisecond)

			pos, _ := file.Seek(0, io.SeekCurrent)
			stat, statErr := file.Stat()

			if statErr != nil || stat.Size() < pos {
				file.Close()
				file = nil
				reader = nil
				gameState = GameState{}
				shouldBroadcast = true
			}

			continue
		}

		log.Printf("read error: %v", err)
		file.Close()
		file = nil
		reader = nil
		time.Sleep(time.Second)
	}
}

func listen() {
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal("failed to create sub filesystem:", err)
	}
	http.Handle("/", http.FileServer(http.FS(subFS)))

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, _ := w.(http.Flusher)
		client := make(chan string, 10)

		clientsMu.Lock()
		clients[client] = true
		clientsMu.Unlock()

		jsonData, err := JSONMarshal(gameState)
		if err == nil {
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}

		defer func() {
			clientsMu.Lock()
			delete(clients, client)
			clientsMu.Unlock()
		}()

		for {
			select {
			case <-r.Context().Done():
				return
			case message := <-client:
				fmt.Fprintf(w, "data: %s\n\n", message)
				flusher.Flush()
			}
		}
	})

	http.HandleFunc("/local-wr", localWRHandler)
	http.HandleFunc("/api/progress", apiProgressHandler)
	http.HandleFunc("/progress", progressPageHandler)

	go func() {
		if err := http.ListenAndServe("127.0.0.1:4433", nil); err != nil {
			log.Fatal("failed to start server:", err)
		}
	}()
	fmt.Println("Server started at http://127.0.0.1:4433")
	fmt.Println("Please make sure to enable `-condebug -conclearlog` in your CS2 launch options and use `!mapoverlay` while in CS2KZ servers for proper functionality.")
}

func onReady() {
	systray.SetTitle("cs2kz-overlay")
	systray.SetTooltip("cs2kz-overlay")

	if len(iconData) > 0 {
		systray.SetIcon(iconData)
	}

	mExit := systray.AddMenuItem("Exit", "Exit the application")

	go func() {
		<-mExit.ClickedCh
		systray.Quit()
	}()
}

func onExit() {}

func main() {
	var logFilePath string
	flag.StringVar(&logFilePath, "log-path", "", "Path to console.log (overrides auto-detection)")
	flag.Parse()

	go watchLog(logFilePath)
	go listen()

	systray.Run(onReady, onExit)
}