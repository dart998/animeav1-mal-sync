from pathlib import Path

main = Path('main.go')
s = main.read_text(encoding='utf-8')

s = s.replace('appVersion = "1.7.0-rc2"', 'appVersion = "1.7.0-rc3"', 1)
s = s.replace('mux.HandleFunc("/api/reverse/resolve", app.reverseResolveAPI)\n', 'mux.HandleFunc("/api/reverse/resolve", app.reverseResolveAPI)\n\tmux.HandleFunc("/api/reverse/manual-match", app.reverseManualMatchAPI)\n', 1)

old = "function manualMatchBox(i){const q=encodeURIComponent(i.source_title||'');return '<div class=\"manual-match\"><div style=\"margin:8px 0 10px\"><a class=\"btn secondary\" target=\"_blank\" rel=\"noopener\" href=\"https://myanimelist.net/anime.php?q='+q+'\">🔎 Buscar «'+esc(i.source_title)+'» en MyAnimeList ↗</a></div><div style=\"display:grid;grid-template-columns:1fr 1fr auto;gap:8px;align-items:end\"><label>MAL ID<input class=\"manual-mal-1\" type=\"number\" min=\"1\" inputmode=\"numeric\" placeholder=\"Obligatorio\"></label><label>MAL ID 2<input class=\"manual-mal-2\" type=\"number\" min=\"1\" inputmode=\"numeric\" placeholder=\"Opcional, temporada dividida\"></label><button type=\"button\" class=\"manual-save\" data-media-id=\"'+esc(i.media_id)+'\">Guardar</button></div><div class=\"manual-result muted\"></div></div>'}"
new = old + "\nfunction reverseManualMatchBox(i){return '<div class=\"manual-match reverse-manual-match\"><div class=\"warn\" style=\"margin:8px 0\">No se ha podido identificar automáticamente la ficha de AnimeAV1. El MAL ID ya es conocido: #'+esc(i.mal_id)+'.</div><div style=\"display:grid;grid-template-columns:1fr auto;gap:8px;align-items:end\"><label>AnimeAV1 ID<input class=\"manual-av1-id\" type=\"number\" min=\"1\" inputmode=\"numeric\" placeholder=\"ID de la ficha en AnimeAV1\"></label><button type=\"button\" class=\"reverse-manual-save\" data-mal-id=\"'+esc(i.mal_id)+'\" data-mal-title=\"'+esc(i.mal_title||i.source_title||'')+'\">Guardar</button></div><div class=\"manual-result muted\"></div></div>'}"
if old not in s:
    raise SystemExit('manualMatchBox anchor not found')
s = s.replace(old, new, 1)

old = "const manual=i.result==='error'?(String(i.message||'').startsWith('Conflicto de episodios')?reverseConflictBox(i):manualMatchBox(i)):'';"
new = "const reverseUnmatched=i.result==='error'&&!i.media_id&&!!i.mal_id&&String(i.message||'').startsWith('sin coincidencia segura en AnimeAV1');const manual=i.result==='error'?(String(i.message||'').startsWith('Conflicto de episodios')?reverseConflictBox(i):(reverseUnmatched?reverseManualMatchBox(i):manualMatchBox(i))):'';"
if old not in s:
    raise SystemExit('resultTable manual anchor not found')
s = s.replace(old, new, 1)

anchor = "function bindManualMatches(){document.querySelectorAll('.manual-save').forEach(b=>b.addEventListener('click',async()=>{"
if anchor not in s:
    raise SystemExit('bindManualMatches anchor not found')
insert = "function bindReverseManualMatches(){document.querySelectorAll('.reverse-manual-save').forEach(b=>b.addEventListener('click',async()=>{const box=b.closest('.reverse-manual-match'),row=b.closest('tr'),av1=box.querySelector('.manual-av1-id').value.trim(),out=box.querySelector('.manual-result');if(!av1){out.textContent='Introduce el ID de AnimeAV1.';return}b.disabled=true;out.textContent='Validando y guardando…';try{const body=new URLSearchParams({media_id:av1,mal_id:b.dataset.malId,mal_title:b.dataset.malTitle});const r=await fetch('/api/reverse/manual-match',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo guardar');out.textContent='✓ Relación guardada. En la siguiente sincronización MAL → AnimeAV1 se usará AnimeAV1 #'+av1+'.';if(lastData?.last?.items)lastData.last.items=lastData.last.items.filter(i=>!(i.result==='error'&&String(i.mal_id)===String(b.dataset.malId)&&!i.media_id));setTimeout(()=>row?.remove(),350);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}))}\n"
s = s.replace(anchor, insert + anchor, 1)

old = "if(kind==='error'||kind==='all'){bindManualMatches();bindReverseConflicts()}}"
new = "if(kind==='error'||kind==='all'){bindManualMatches();bindReverseManualMatches();bindReverseConflicts()}}"
if old not in s:
    raise SystemExit('openResults bind anchor not found')
s = s.replace(old, new, 1)
main.write_text(s, encoding='utf-8')

reverse = Path('reverse_runtime.go')
r = reverse.read_text(encoding='utf-8')
append = r'''

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
	mediaID := IDString(strings.TrimSpace(req.FormValue("media_id")))
	malID, _ := strconv.Atoi(strings.TrimSpace(req.FormValue("mal_id")))
	if mediaID == "" || malID <= 0 {
		http.Error(w, "media_id de AnimeAV1 y mal_id son obligatorios", http.StatusBadRequest)
		return
	}

	// Valida que el MAL ID exista y obtiene su título canónico. El ID de AnimeAV1
	// lo aporta explícitamente el usuario porque este endpoint existe precisamente
	// para resolver casos donde el buscador automático de AnimeAV1 no es concluyente.
	var anime MALAnime
	fields := "id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status"
	if err := a.malRequestContext(req.Context(), http.MethodGet, fmt.Sprintf("/anime/%d?fields=%s", malID, fields), nil, &anime); err != nil {
		http.Error(w, "ID de MAL no válido: "+err.Error(), http.StatusBadGateway)
		return
	}

	entry := CacheEntry{
		MediaID: mediaID,
		MALID: anime.ID,
		MALTitle: anime.Title,
		MatchType: "manual_reverse",
		MatchScore: 999,
		SourceTitle: normalize(anime.Title),
		LastValidated: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		MatcherVersion: appVersion,
		SearchStrategy: "manual_animeav1_id",
	}
	if seen, status := animeState(anime); true {
		entry.MALSeen = seen
		entry.MALStatus = status
	}
	a.cachePut(entry)
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "reverse_manual_match_saved", "media_id": mediaID, "mal_id": anime.ID, "mal_title": anime.Title})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "entry": entry})
}
'''
if 'func (a *App) reverseManualMatchAPI' not in r:
    r += append
reverse.write_text(r, encoding='utf-8')

Path('VERSION').write_text('1.7.0-rc3\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8').replace('ovelayos/animeav1-mal-sync:1.7.0-rc2', 'ovelayos/animeav1-mal-sync:1.7.0-rc3')
compose.write_text(c, encoding='utf-8')
