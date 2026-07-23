package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// manualCacheEntryAPI permite fijar uno o dos IDs de MyAnimeList cuando el
// matcher automático no encuentra una coincidencia segura.
func (a *App) manualCacheEntryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}

	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		http.Error(w, "detén la sincronización antes de modificar la caché", http.StatusConflict)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulario no válido", http.StatusBadRequest)
		return
	}
	mediaID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("media_id")))
	if err != nil || mediaID <= 0 {
		http.Error(w, "media_id no válido", http.StatusBadRequest)
		return
	}
	malID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("mal_id")))
	if err != nil || malID <= 0 {
		http.Error(w, "mal_id no válido", http.StatusBadRequest)
		return
	}
	malID2 := 0
	if raw := strings.TrimSpace(r.FormValue("mal_id_2")); raw != "" {
		malID2, err = strconv.Atoi(raw)
		if err != nil || malID2 <= 0 || malID2 == malID {
			http.Error(w, "mal_id_2 no válido", http.StatusBadRequest)
			return
		}
	}

	old, ok := a.cacheGet(mediaID)
	if !ok {
		http.Error(w, "media_id no encontrado en la caché; ejecuta antes una sincronización", http.StatusNotFound)
		return
	}

	fetchAnime := func(id int) (MALAnime, error) {
		var anime MALAnime
		fields := "id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status"
		err := a.malRequestContext(r.Context(), http.MethodGet, fmt.Sprintf("/anime/%d?fields=%s", id, fields), nil, &anime)
		return anime, err
	}

	anime, err := fetchAnime(malID)
	if err != nil {
		http.Error(w, "no se pudo validar mal_id: "+err.Error(), http.StatusBadGateway)
		return
	}
	var anime2 MALAnime
	if malID2 > 0 {
		anime2, err = fetchAnime(malID2)
		if err != nil {
			http.Error(w, "no se pudo validar mal_id_2: "+err.Error(), http.StatusBadGateway)
			return
		}
	}

	seen, status := animeState(anime)
	entry := old
	entry.MALID = anime.ID
	entry.MALTitle = anime.Title
	entry.MatchScore = 999
	entry.MatchType = "manual"
	entry.SearchStrategy = "manual"
	entry.MALSeen = seen
	entry.MALStatus = status
	entry.MALID2 = 0
	entry.MALTitle2 = ""
	entry.MAL2Episodes = 0
	entry.MAL2Seen = 0
	entry.MAL2Status = ""
	if anime2.ID > 0 {
		seen2, status2 := animeState(anime2)
		entry.MALID2 = anime2.ID
		entry.MALTitle2 = anime2.Title
		entry.MAL2Episodes = anime2.NumEpisodes
		entry.MAL2Seen = seen2
		entry.MAL2Status = status2
	}
	entry.LastValidated = time.Now().Unix()
	entry.UpdatedAt = time.Now().Unix()
	entry.MatcherVersion = appVersion
	entry.NegativeUntil = 0
	entry.NegativeReason = ""
	a.cachePut(entry)

	a.appendHistory(map[string]any{
		"ts":           time.Now().Unix(),
		"event":        "cache_entry_manual",
		"media_id":     mediaID,
		"source_title": entry.SourceTitle,
		"mal_id":       entry.MALID,
		"mal_title":    entry.MALTitle,
		"mal_id_2":     entry.MALID2,
		"mal_title_2":  entry.MALTitle2,
		"message":      "Coincidencia fijada manualmente",
	})

	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("X-Requested-With") == "fetch" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "entry": entry})
		return
	}
	redirectHome(w, r)
}
