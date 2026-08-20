package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ReverseConflict struct {
	MediaID       IDString `json:"media_id"`
	MALID         int      `json:"mal_id"`
	AnimeAV1Title string   `json:"animeav1_title"`
	MALTitle      string   `json:"mal_title"`
	AnimeAV1Seen  int      `json:"animeav1_seen"`
	MALSeen       int      `json:"mal_seen"`
	AnimeAV1Slug  string   `json:"animeav1_slug"`
	Reason        string   `json:"reason"`
}

func (a *App) animeAV1SetEpisode(ctx context.Context, cookie string, mediaID IDString, status, episode int) error {
	// Capturado en un HAR real de AnimeAV1 (SvelteKit/Superforms):
	// __superform_json=[{"status":1,"rating":2,"episode":3,"notes":4,"startDate":2,"endDate":2,"private":5,"mediaId":6},status,null,episode,"",false,mediaId]
	payload := []any{
		map[string]int{"status": 1, "rating": 2, "episode": 3, "notes": 4, "startDate": 2, "endDate": 2, "private": 5, "mediaId": 6},
		status, nil, episode, "", false, mediaID,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	vals := url.Values{}
	vals.Set("__superform_json", string(b))
	vals.Set("__superform_id", getenv("ANIMEAV1_SUPERFORM_ID", "ab6m4yn8"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://animeav1.com/cuenta/listas?/library", strings.NewReader(vals.Encode()))
	if err != nil {
		return err
	}
	browserHeaders(req, cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://animeav1.com")
	req.Header.Set("Referer", "https://animeav1.com/cuenta/listas")
	req.Header.Set("x-sveltekit-action", "true")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("AnimeAV1 progreso HTTP %d", resp.StatusCode)
	}
	return nil
}

func (a *App) reverseConflictPath() string {
	return filepath.Join(a.dataDir, "reverse_conflicts.json")
}

func (a *App) saveReverseConflicts(items []ReverseConflict) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.reverseConflictPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.reverseConflictPath())
}

func (a *App) loadReverseConflicts() []ReverseConflict {
	b, err := os.ReadFile(a.reverseConflictPath())
	if err != nil {
		return nil
	}
	var out []ReverseConflict
	_ = json.Unmarshal(b, &out)
	return out
}

func (a *App) runReverseSync(trigger string) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.running = true
	a.cancelSync = cancel
	a.progressProcessed = 0
	a.progressTotal = 0
	a.progressMessage = "Preparando sincronización MAL → AnimeAV1"
	a.progressTrigger = trigger + ":reverse"
	cookie := a.state.Settings.Cookie
	dry := a.state.Settings.DryRun
	started := time.Now().Unix()
	a.state.Last = LastRun{Status: "running", Started: started, Message: "Sincronización inversa " + trigger}
	a.save()
	a.mu.Unlock()
	defer cancel()

	last := LastRun{Status: "ok", Started: started}
	malItems, err := a.fetchMALList(ctx)
	if err != nil {
		last.Status, last.Errors, last.Message = "error", 1, err.Error()
		a.finish(last)
		return
	}
	avItems, err := a.scrapeContext(ctx, cookie)
	if err != nil {
		last.Status, last.Errors, last.Message = "error", 1, err.Error()
		a.finish(last)
		return
	}
	avByID := map[string]AVItem{}
	for _, item := range avItems {
		avByID[string(item.MediaID)] = item
	}
	malByID := map[int]MALListItem{}
	for _, item := range malItems {
		malByID[item.ID] = item
	}
	last.Found = len(malItems)
	a.mu.Lock()
	a.progressTotal = len(malItems)
	a.mu.Unlock()

	conflicts := make([]ReverseConflict, 0)
	claimedMedia := map[string]MALListItem{}
	processedMAL := map[int]bool{}

	for idx, original := range malItems {
		if processedMAL[original.ID] {
			continue
		}
		if ctx.Err() != nil {
			last.Status = "cancelled"
			last.Message = "Sincronización inversa detenida"
			break
		}

		mal := original
		var splitSecond *MALListItem
		forcedMediaID := IDString("")
		forcedScore := 0
		cached, cachedOK, cacheErr := a.cachedEntryForMAL(original.ID)
		if cacheErr != nil {
			last.Errors++
			last.Items = append(last.Items, RunItem{MALID: original.ID, MALTitle: original.Title, SourceTitle: original.Title, Result: "error", Message: cacheErr.Error(), Direction: "reverse", ErrorType: "cache_ambiguous"})
			processedMAL[original.ID] = true
			continue
		}
		if cachedOK && cached.MALID2 > 0 {
			first, ok1 := malByID[cached.MALID]
			second, ok2 := malByID[cached.MALID2]
			if ok1 && ok2 {
				mal = aggregateSplitMAL(first, second)
				splitCopy := second
				splitSecond = &splitCopy
				forcedMediaID = cached.MediaID
				forcedScore = 999
				processedMAL[first.ID] = true
				processedMAL[second.ID] = true
			}
		}
		if !processedMAL[original.ID] {
			processedMAL[original.ID] = true
		}

		a.mu.Lock()
		a.progressProcessed = idx
		a.progressMessage = "MAL → AnimeAV1: " + mal.Title
		a.mu.Unlock()

		baseItem := func() RunItem {
			ri := RunItem{MALID: mal.ID, MALTitle: mal.Title, SourceTitle: mal.Title, Direction: "reverse"}
			if splitSecond != nil {
				ri.MALID = cached.MALID
				ri.MALTitle = malByID[cached.MALID].Title
				ri.MALID2 = cached.MALID2
				ri.MALTitle2 = splitSecond.Title
			}
			return ri
		}

		if mal.AirStatus == "not_yet_aired" {
			last.Skipped++
			msg := "Próximamente · MAL indica que todavía no se ha estrenado; no se busca en AnimeAV1"
			if mal.StartDate != "" {
				msg += " · estreno: " + mal.StartDate
			}
			ri := baseItem()
			ri.From, ri.To, ri.Status, ri.Result, ri.Message = mal.Seen, mal.Seen, mal.Status, "skipped", msg
			last.Items = append(last.Items, ri)
			continue
		}

		status, err := avStatusFromMAL(mal.Status)
		if err != nil {
			last.Errors++
			ri := baseItem()
			ri.Result, ri.Message, ri.ErrorType = "error", err.Error(), "mal_status"
			last.Items = append(last.Items, ri)
			continue
		}

		var mediaID IDString
		var score int
		if forcedMediaID != "" {
			mediaID, score = forcedMediaID, forcedScore
		} else {
			mediaID, _, score, err = a.resolveAnimeAV1Media(ctx, cookie, mal)
			if err != nil {
				last.Errors++
				last.Unmatched = append(last.Unmatched, mal.Title+": "+err.Error())
				ri := baseItem()
				ri.MatchScore, ri.Result, ri.Message = score, "error", err.Error()
				if mediaID == "" {
					ri.ErrorType = "animeav1_unmatched"
				}
				last.Items = append(last.Items, ri)
				continue
			}
		}

		claimKey := string(mediaID)
		if previous, ok := claimedMedia[claimKey]; ok && previous.ID != mal.ID {
			msg := fmt.Sprintf("Colisión de coincidencia: MAL #%d (%s) y MAL #%d (%s) apuntan al mismo AnimeAV1 media_id=%s. No se modifica la segunda entrada; requiere revisión manual.", previous.ID, previous.Title, mal.ID, mal.Title, mediaID)
			last.Errors++
			ri := baseItem()
			ri.MediaID, ri.MatchScore, ri.Result, ri.Message, ri.ErrorType = mediaID, score, "error", msg, "media_collision"
			last.Items = append(last.Items, ri)
			continue
		}
		claimedMedia[claimKey] = mal

		av, exists := avByID[string(mediaID)]
		if !exists {
			if !dry {
				if err := a.animeAV1UpdateStatus(ctx, cookie, mediaID, status); err != nil {
					last.Errors++
					ri := baseItem()
					ri.MediaID, ri.Result, ri.Message, ri.ErrorType = mediaID, "error", "No se pudo añadir a AnimeAV1: "+err.Error(), "animeav1_write"
					last.Items = append(last.Items, ri)
					continue
				}
				if mal.Seen > 0 {
					if err := a.animeAV1SetEpisode(ctx, cookie, mediaID, status, mal.Seen); err != nil {
						last.Errors++
						ri := baseItem()
						ri.MediaID, ri.From, ri.To, ri.Result, ri.Message, ri.ErrorType = mediaID, 0, mal.Seen, "error", "Añadido, pero no se pudo guardar progreso: "+err.Error(), "animeav1_write"
						last.Items = append(last.Items, ri)
						continue
					}
				}
			}
			last.Updated++
			msg := "Añadido a AnimeAV1 con estado y progreso de MAL"
			if splitSecond != nil {
				msg = "Temporada partida reconocida · " + msg
			}
			if dry {
				msg = "Simulado · " + msg
			}
			ri := baseItem()
			ri.MediaID, ri.From, ri.To, ri.Status, ri.Result, ri.Message = mediaID, 0, mal.Seen, mal.Status, "updated", msg
			last.Items = append(last.Items, ri)
			continue
		}

		if av.Seen != mal.Seen {
			resolutionMALID := mal.ID
			if splitSecond != nil {
				resolutionMALID = cached.MALID
			}
			if saved, ok := a.reverseResolution(av.MediaID, resolutionMALID); ok {
				if saved.PreferredSource == ReverseTruthMAL && !dry {
					if err := a.animeAV1SetEpisode(ctx, cookie, av.MediaID, status, mal.Seen); err != nil {
						last.Errors++
						ri := baseItem()
						ri.MediaID, ri.From, ri.To, ri.Result, ri.Message, ri.ErrorType = av.MediaID, av.Seen, mal.Seen, "error", err.Error(), "animeav1_write"
						last.Items = append(last.Items, ri)
						continue
					}
				}
				last.Skipped++
				ri := baseItem()
				ri.MediaID, ri.SourceTitle, ri.From, ri.To, ri.Result, ri.Message = av.MediaID, av.Title, av.Seen, mal.Seen, "skipped", "Conflicto resuelto previamente · fuente: "+saved.PreferredSource
				last.Items = append(last.Items, ri)
				continue
			}
			conflicts = append(conflicts, ReverseConflict{MediaID: av.MediaID, MALID: resolutionMALID, AnimeAV1Title: av.Title, MALTitle: mal.Title, AnimeAV1Seen: av.Seen, MALSeen: mal.Seen, AnimeAV1Slug: av.Slug, Reason: "El recuento de episodios difiere entre AnimeAV1 y MAL"})
			last.Errors++
			ri := baseItem()
			ri.MediaID, ri.SourceTitle, ri.From, ri.To, ri.Result, ri.Message, ri.ErrorType = av.MediaID, av.Title, av.Seen, mal.Seen, "error", "Conflicto de episodios: requiere decisión manual", "episode_conflict"
			last.Items = append(last.Items, ri)
			continue
		}

		if av.Status != status {
			if !dry {
				if err := a.animeAV1UpdateStatus(ctx, cookie, av.MediaID, status); err != nil {
					last.Errors++
					ri := baseItem()
					ri.MediaID, ri.SourceTitle, ri.Result, ri.Message, ri.ErrorType = av.MediaID, av.Title, "error", err.Error(), "animeav1_write"
					last.Items = append(last.Items, ri)
					continue
				}
			}
			last.Updated++
			msg := "Estado actualizado en AnimeAV1"
			if splitSecond != nil {
				msg = "Temporada partida reconocida · " + msg
			}
			if dry {
				msg = "Simulado · estado"
			}
			ri := baseItem()
			ri.MediaID, ri.SourceTitle, ri.From, ri.To, ri.Status, ri.Result, ri.Message = av.MediaID, av.Title, av.Seen, mal.Seen, mal.Status, "updated", msg
			last.Items = append(last.Items, ri)
		} else {
			last.Skipped++
			ri := baseItem()
			ri.MediaID, ri.SourceTitle, ri.From, ri.To, ri.Status, ri.Result = av.MediaID, av.Title, av.Seen, mal.Seen, mal.Status, "skipped"
			if splitSecond != nil {
				ri.Message = "Temporada partida reconocida · sin cambios"
			} else {
				ri.Message = "Sin cambios"
			}
			last.Items = append(last.Items, ri)
		}
		a.mu.Lock()
		a.progressProcessed = idx + 1
		a.mu.Unlock()
	}

	_ = a.saveReverseConflicts(conflicts)
	if last.Status != "cancelled" {
		if last.Errors > 0 {
			last.Status = "partial"
		}
		last.Message = fmt.Sprintf("MAL → AnimeAV1: encontrados %d, actualizados %d, omitidos %d, conflictos/errores %d", last.Found, last.Updated, last.Skipped, last.Errors)
	}
	a.finish(last)
}

func (a *App) reverseSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	if r.FormValue("interval") != "" {
		a.saveSettingsNoRedirect(r)
	}
	go a.runReverseSync("manual")
	redirectHome(w, r)
}

func (a *App) reverseConflictsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"items": a.loadReverseConflicts()})
}

func (a *App) reverseResolveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mediaID := IDString(strings.TrimSpace(r.FormValue("media_id")))
	malID, _ := strconv.Atoi(r.FormValue("mal_id"))
	preferred := strings.TrimSpace(r.FormValue("preferred_source"))
	avSeen, _ := strconv.Atoi(r.FormValue("animeav1_seen"))
	malSeen, _ := strconv.Atoi(r.FormValue("mal_seen"))
	v := ReverseResolution{MediaID: mediaID, MALID: malID, AnimeAV1Title: r.FormValue("animeav1_title"), MALTitle: r.FormValue("mal_title"), PreferredSource: preferred, AnimeAV1SeenAtSet: avSeen, MALSeenAtSet: malSeen}
	if err := a.setReverseResolution(v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	items := a.loadReverseConflicts()
	kept := items[:0]
	for _, c := range items {
		if !(c.MediaID == mediaID && c.MALID == malID) {
			kept = append(kept, c)
		}
	}
	_ = a.saveReverseConflicts(kept)
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "resolution": v})
}

func animeAV1SlugFromRef(raw string) (string, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return "", errors.New("falta la URL o slug de AnimeAV1")
	}
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil {
			return "", errors.New("URL de AnimeAV1 no válida")
		}
		host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
		if host != "animeav1.com" {
			return "", errors.New("la URL debe pertenecer a animeav1.com")
		}
		ref = u.Path
	} else {
		ref = strings.TrimPrefix(ref, "animeav1.com/")
		ref = strings.TrimPrefix(ref, "www.animeav1.com/")
	}
	ref = strings.Trim(ref, "/")
	if strings.HasPrefix(ref, "media/") {
		ref = strings.TrimPrefix(ref, "media/")
	}
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		ref = ref[:i]
	}
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.ContainsAny(ref, " ?#") {
		return "", errors.New("slug de AnimeAV1 no válido")
	}
	return ref, nil
}

func (a *App) reverseManualMatchAPI(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if req.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	if err := req.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	malID, _ := strconv.Atoi(strings.TrimSpace(req.FormValue("mal_id")))
	if malID <= 0 {
		http.Error(w, "mal_id es obligatorio", http.StatusBadRequest)
		return
	}
	slug, err := animeAV1SlugFromRef(req.FormValue("animeav1_ref"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var anime MALAnime
	fields := "id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status"
	if err := a.malRequestContext(req.Context(), http.MethodGet, fmt.Sprintf("/anime/%d?fields=%s", malID, fields), nil, &anime); err != nil {
		http.Error(w, "ID de MAL no válido: "+err.Error(), http.StatusBadGateway)
		return
	}

	a.mu.Lock()
	cookie := a.state.Settings.Cookie
	a.mu.Unlock()
	queries := []string{strings.ReplaceAll(slug, "-", " "), anime.Title}
	seenQueries := map[string]bool{}
	var match animeAV1SearchItem
	for _, q := range queries {
		key := normalize(q)
		if key == "" || seenQueries[key] {
			continue
		}
		seenQueries[key] = true
		items, searchErr := a.animeAV1Search(req.Context(), cookie, q)
		if searchErr != nil {
			continue
		}
		for _, candidate := range items {
			if strings.EqualFold(strings.Trim(candidate.Slug, "/"), slug) {
				match = candidate
				break
			}
		}
		if match.ID != "" {
			break
		}
	}
	if match.ID == "" {
		http.Error(w, "no se encontró en AnimeAV1 una ficha con el slug "+slug, http.StatusNotFound)
		return
	}

	if existing, ok, lookupErr := a.cachedEntryForMAL(anime.ID); lookupErr != nil {
		http.Error(w, lookupErr.Error(), http.StatusConflict)
		return
	} else if ok && existing.MediaID != match.ID {
		http.Error(w, fmt.Sprintf("MAL #%d ya está asociado a AnimeAV1 media_id=%s; elimina o corrige esa coincidencia antes de asignarlo a media_id=%s", anime.ID, existing.MediaID, match.ID), http.StatusConflict)
		return
	}

	entry := CacheEntry{
		MediaID:        match.ID,
		MALID:          anime.ID,
		MALTitle:       anime.Title,
		MatchType:      "manual_reverse",
		MatchScore:     999,
		SourceTitle:    normalize(match.Title),
		LastValidated:  time.Now().Unix(),
		UpdatedAt:      time.Now().Unix(),
		MatcherVersion: appVersion,
		SearchStrategy: "manual_animeav1_slug",
	}
	entry.MALSeen, entry.MALStatus = animeState(anime)
	a.cachePut(entry)
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "reverse_manual_match_saved", "media_id": match.ID, "animeav1_slug": slug, "animeav1_title": match.Title, "mal_id": anime.ID, "mal_title": anime.Title})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "entry": entry, "slug": slug, "animeav1_title": match.Title})
}
