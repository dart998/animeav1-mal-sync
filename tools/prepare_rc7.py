from pathlib import Path
import re

VERSION = "1.7.0-rc7"


def replace_go_func(src: str, signature: str, replacement: str) -> str:
    start = src.find(signature)
    if start < 0:
        raise SystemExit(f"function anchor not found: {signature}")
    brace = src.find('{', start)
    if brace < 0:
        raise SystemExit(f"opening brace not found: {signature}")
    depth = 0
    in_string = False
    in_raw = False
    escaped = False
    i = brace
    while i < len(src):
        c = src[i]
        if in_raw:
            if c == '`':
                in_raw = False
            i += 1
            continue
        if in_string:
            if escaped:
                escaped = False
            elif c == '\\':
                escaped = True
            elif c == '"':
                in_string = False
            i += 1
            continue
        if c == '`':
            in_raw = True
        elif c == '"':
            in_string = True
        elif c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0:
                return src[:start] + replacement.rstrip() + "\n" + src[i+1:]
        i += 1
    raise SystemExit(f"unterminated function: {signature}")


# main.go: version, explicit reverse error metadata, and direction-safe UI.
p = Path('main.go')
s = p.read_text(encoding='utf-8')
s = s.replace('appVersion = "1.7.0-rc6"', f'appVersion = "{VERSION}"', 1)
old = '''\tMessage     string   `json:"message,omitempty"`\n}'''
new = '''\tMessage     string   `json:"message,omitempty"`\n\tDirection   string   `json:"direction,omitempty"`\n\tErrorType   string   `json:"error_type,omitempty"`\n}'''
if old not in s:
    raise SystemExit('RunItem anchor not found')
s = s.replace(old, new, 1)

pattern = re.compile(r'''function reverseManualMatchBox\(i\)\{return .*?\}\nfunction animeAV1IDLink''', re.S)
replacement = '''function reverseManualMatchBox(i){const q=encodeURIComponent(i.mal_title||i.source_title||'');return '<div class="manual-match reverse-manual-match"><div class="warn" style="margin:8px 0">No se ha podido identificar automáticamente la ficha de AnimeAV1. El MAL ID ya es conocido: #'+esc(i.mal_id)+'.</div><div style="margin:8px 0 10px"><a class="btn secondary" target="_blank" rel="noopener" href="https://animeav1.com/catalogo" title="Abrir el catálogo de AnimeAV1">🔎 Buscar en AnimeAV1 ↗</a> <span class="muted">Busca: '+esc(i.mal_title||i.source_title||'')+'</span></div><div style="display:grid;grid-template-columns:1fr auto;gap:8px;align-items:end"><label>URL o slug de AnimeAV1<input class="manual-av1-ref" type="text" placeholder="https://animeav1.com/media/... o slug"></label><button type="button" class="reverse-manual-save" data-mal-id="'+esc(i.mal_id)+'" data-mal-title="'+esc(i.mal_title||i.source_title||'')+'">Guardar</button></div><div class="manual-result muted"></div></div>'}\nfunction animeAV1IDLink'''
s, n = pattern.subn(replacement, s, count=1)
if n != 1:
    raise SystemExit('reverseManualMatchBox anchor not found')
old = "const reverseUnmatched=i.result==='error'&&!i.media_id&&!!i.mal_id&&String(i.message||'').startsWith('sin coincidencia segura en AnimeAV1');"
new = "const reverseUnmatched=i.result==='error'&&i.direction==='reverse'&&i.error_type==='animeav1_unmatched';"
if old not in s:
    raise SystemExit('reverseUnmatched UI anchor not found')
s = s.replace(old, new, 1)
p.write_text(s, encoding='utf-8')

# reverse_sync.go: deterministic cache lookup and split-season aggregation.
p = Path('reverse_sync.go')
s = p.read_text(encoding='utf-8')
anchor = '''func (a *App) cachedMediaIDForMAL(malID int) (IDString, bool) {'''
idx = s.find(anchor)
if idx < 0:
    raise SystemExit('cachedMediaIDForMAL anchor not found')
# Replace the old helper with stricter helpers.
new_helpers = r'''func (a *App) cachedEntryForMAL(malID int) (CacheEntry, bool, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	var found CacheEntry
	have := false
	for _, entry := range a.cache {
		if entry.MALID != malID && entry.MALID2 != malID {
			continue
		}
		if have && found.MediaID != entry.MediaID {
			return CacheEntry{}, false, fmt.Errorf("MAL #%d está asociado a más de una ficha AnimeAV1 (%s y %s); corrige la caché antes de sincronizar", malID, found.MediaID, entry.MediaID)
		}
		found = entry
		have = true
	}
	return found, have, nil
}

func (a *App) cachedMediaIDForMAL(malID int) (IDString, bool) {
	entry, ok, err := a.cachedEntryForMAL(malID)
	if err != nil || !ok {
		return "", false
	}
	return entry.MediaID, true
}

func aggregateSplitMAL(first, second MALListItem) MALListItem {
	out := first
	out.Seen = first.Seen + second.Seen
	out.Episodes = first.Episodes + second.Episodes
	out.Title = first.Title + " + " + second.Title
	out.Aliases = append(append([]string{}, first.Aliases...), second.Aliases...)
	if first.Status == "completed" && second.Status == "completed" {
		out.Status = "completed"
	} else if second.Status == "watching" || first.Status == "watching" {
		out.Status = "watching"
	} else if second.Status == "on_hold" || first.Status == "on_hold" {
		out.Status = "on_hold"
	} else if second.Status == "dropped" {
		out.Status = "dropped"
	} else if first.Seen+second.Seen > 0 {
		out.Status = "watching"
	} else if second.Status != "" {
		out.Status = second.Status
	}
	// Una coincidencia partida ya validada representa una sola ficha de AnimeAV1.
	// No se excluye toda la ficha porque una de las dos partes aún no se haya emitido.
	if first.AirStatus == "not_yet_aired" && second.AirStatus == "not_yet_aired" {
		out.AirStatus = "not_yet_aired"
	} else {
		out.AirStatus = ""
	}
	return out
}
'''
s = replace_go_func(s, anchor, new_helpers)
old = '''\tif mediaID, ok := a.cachedMediaIDForMAL(item.ID); ok {\n\t\treturn mediaID, "cache", 999, nil\n\t}'''
new = '''\tif entry, ok, err := a.cachedEntryForMAL(item.ID); err != nil {\n\t\treturn "", "", -1, err\n\t} else if ok {\n\t\treturn entry.MediaID, "cache", 999, nil\n\t}'''
if old not in s:
    raise SystemExit('resolve cache anchor not found')
s = s.replace(old, new, 1)
p.write_text(s, encoding='utf-8')

# reverse_runtime.go: group validated MAL split seasons before matching/collision logic.
p = Path('reverse_runtime.go')
s = p.read_text(encoding='utf-8')
run_func = r'''func (a *App) runReverseSync(trigger string) {
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
'''
s = replace_go_func(s, 'func (a *App) runReverseSync(trigger string)', run_func)

# Reject new manual mappings that would make one MAL ID point to multiple AV entries.
needle = '''\tif match.ID == "" {\n\t\thttp.Error(w, "no se encontró en AnimeAV1 una ficha con el slug "+slug, http.StatusNotFound)\n\t\treturn\n\t}\n\n\tentry := CacheEntry{'''
replacement = '''\tif match.ID == "" {\n\t\thttp.Error(w, "no se encontró en AnimeAV1 una ficha con el slug "+slug, http.StatusNotFound)\n\t\treturn\n\t}\n\n\tif existing, ok, lookupErr := a.cachedEntryForMAL(anime.ID); lookupErr != nil {\n\t\thttp.Error(w, lookupErr.Error(), http.StatusConflict)\n\t\treturn\n\t} else if ok && existing.MediaID != match.ID {\n\t\thttp.Error(w, fmt.Sprintf("MAL #%d ya está asociado a AnimeAV1 media_id=%s; elimina o corrige esa coincidencia antes de asignarlo a media_id=%s", anime.ID, existing.MediaID, match.ID), http.StatusConflict)\n\t\treturn\n\t}\n\n\tentry := CacheEntry{'''
if needle not in s:
    raise SystemExit('manual reverse validation anchor not found')
s = s.replace(needle, replacement, 1)
p.write_text(s, encoding='utf-8')

# Focused unit tests for the split-season behavior.
Path('reverse_sync_rc7_test.go').write_text(r'''package main

import "testing"

func TestAggregateSplitMAL(t *testing.T) {
	first := MALListItem{ID: 100, Title: "Season Part 1", Episodes: 13, Seen: 13, Status: "completed", AirStatus: "finished_airing"}
	second := MALListItem{ID: 101, Title: "Season Part 2", Episodes: 12, Seen: 12, Status: "completed", AirStatus: "finished_airing"}
	got := aggregateSplitMAL(first, second)
	if got.Seen != 25 || got.Episodes != 25 || got.Status != "completed" {
		t.Fatalf("unexpected aggregate: seen=%d episodes=%d status=%s", got.Seen, got.Episodes, got.Status)
	}
}

func TestAggregateSplitMALPartialIsWatching(t *testing.T) {
	first := MALListItem{ID: 100, Episodes: 12, Seen: 12, Status: "completed"}
	second := MALListItem{ID: 101, Episodes: 12, Seen: 0, Status: "plan_to_watch"}
	got := aggregateSplitMAL(first, second)
	if got.Seen != 12 || got.Status != "watching" {
		t.Fatalf("unexpected aggregate: seen=%d status=%s", got.Seen, got.Status)
	}
}

func TestCachedEntryForMALRejectsAmbiguousMappings(t *testing.T) {
	a := &App{cache: map[string]CacheEntry{
		"10": {MediaID: "10", MALID: 500},
		"11": {MediaID: "11", MALID: 500},
	}}
	if _, _, err := a.cachedEntryForMAL(500); err == nil {
		t.Fatal("expected ambiguous cache mapping error")
	}
}
''', encoding='utf-8')

Path('VERSION').write_text(VERSION + '\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
cs = compose.read_text(encoding='utf-8')
cs = re.sub(r'image: ovelayos/animeav1-mal-sync:[^\s]+', f'image: ovelayos/animeav1-mal-sync:{VERSION}', cs, count=1)
compose.write_text(cs, encoding='utf-8')
