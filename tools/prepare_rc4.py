from pathlib import Path
import re

main = Path('main.go')
s = main.read_text(encoding='utf-8')
s = s.replace('appVersion = "1.7.0-rc3"', 'appVersion = "1.7.0-rc4"', 1)

old_box = '''function reverseManualMatchBox(i){return '<div class="manual-match reverse-manual-match"><div class="warn" style="margin:8px 0">No se ha podido identificar automáticamente la ficha de AnimeAV1. El MAL ID ya es conocido: #'+esc(i.mal_id)+'.</div><div style="display:grid;grid-template-columns:1fr auto;gap:8px;align-items:end"><label>AnimeAV1 ID<input class="manual-av1-id" type="number" min="1" inputmode="numeric" placeholder="ID de la ficha en AnimeAV1"></label><button type="button" class="reverse-manual-save" data-mal-id="'+esc(i.mal_id)+'" data-mal-title="'+esc(i.mal_title||i.source_title||'')+'">Guardar</button></div><div class="manual-result muted"></div></div>'}'''
new_box = '''function reverseManualMatchBox(i){return '<div class="manual-match reverse-manual-match"><div class="warn" style="margin:8px 0">No se ha podido identificar automáticamente la ficha de AnimeAV1. El MAL ID ya es conocido: #'+esc(i.mal_id)+'.</div><div style="display:grid;grid-template-columns:1fr auto;gap:8px;align-items:end"><label>URL o slug de AnimeAV1<input class="manual-av1-ref" type="text" placeholder="https://animeav1.com/media/... o slug"></label><button type="button" class="reverse-manual-save" data-mal-id="'+esc(i.mal_id)+'" data-mal-title="'+esc(i.mal_title||i.source_title||'')+'">Guardar</button></div><div class="manual-result muted"></div></div>'}'''
if old_box not in s:
    raise SystemExit('reverseManualMatchBox anchor not found')
s = s.replace(old_box, new_box, 1)

old_bind = '''function bindReverseManualMatches(){document.querySelectorAll('.reverse-manual-save').forEach(b=>b.addEventListener('click',async()=>{const box=b.closest('.reverse-manual-match'),row=b.closest('tr'),av1=box.querySelector('.manual-av1-id').value.trim(),out=box.querySelector('.manual-result');if(!av1){out.textContent='Introduce el ID de AnimeAV1.';return}b.disabled=true;out.textContent='Validando y guardando…';try{const body=new URLSearchParams({media_id:av1,mal_id:b.dataset.malId,mal_title:b.dataset.malTitle});const r=await fetch('/api/reverse/manual-match',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo guardar');out.textContent='✓ Relación guardada. En la siguiente sincronización MAL → AnimeAV1 se usará AnimeAV1 #'+av1+'.';if(lastData?.last?.items)lastData.last.items=lastData.last.items.filter(i=>!(i.result==='error'&&String(i.mal_id)===String(b.dataset.malId)&&!i.media_id));setTimeout(()=>row?.remove(),350);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}))}'''
new_bind = '''function bindReverseManualMatches(){document.querySelectorAll('.reverse-manual-save').forEach(b=>b.addEventListener('click',async()=>{const box=b.closest('.reverse-manual-match'),row=b.closest('tr'),ref=box.querySelector('.manual-av1-ref').value.trim(),out=box.querySelector('.manual-result');if(!ref){out.textContent='Introduce la URL o el slug de AnimeAV1.';return}b.disabled=true;out.textContent='Resolviendo ficha y guardando…';try{const body=new URLSearchParams({animeav1_ref:ref,mal_id:b.dataset.malId,mal_title:b.dataset.malTitle});const r=await fetch('/api/reverse/manual-match',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo guardar');out.textContent='✓ Relación guardada con '+(x.entry?.source_title||'AnimeAV1')+' (ID '+(x.entry?.media_id||'')+').';if(lastData?.last?.items)lastData.last.items=lastData.last.items.filter(i=>!(i.result==='error'&&String(i.mal_id)===String(b.dataset.malId)&&!i.media_id));setTimeout(()=>row?.remove(),350);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}))}'''
if old_bind not in s:
    raise SystemExit('bindReverseManualMatches anchor not found')
s = s.replace(old_bind, new_bind, 1)

old_cell = '''<td>'+esc(i.source_title)+'<br>'+animeAV1IDLink(i.media_id)+'</td>'''
new_cell = '''<td>'+(i.media_id?(esc(i.source_title)+'<br>'+animeAV1IDLink(i.media_id)):'—<br><span class="muted">No identificado</span>')+'</td>'''
if old_cell not in s:
    raise SystemExit('AnimeAV1 result cell anchor not found')
s = s.replace(old_cell, new_cell, 1)
main.write_text(s, encoding='utf-8')

reverse = Path('reverse_runtime.go')
r = reverse.read_text(encoding='utf-8')
pattern = re.compile(r'func \(a \*App\) reverseManualMatchAPI\(w http\.ResponseWriter, req \*http\.Request\) \{.*?\n\}', re.S)
replacement = r'''func animeAV1SlugFromRef(raw string) (string, error) {
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

	entry := CacheEntry{
		MediaID: match.ID,
		MALID: anime.ID,
		MALTitle: anime.Title,
		MatchType: "manual_reverse",
		MatchScore: 999,
		SourceTitle: normalize(match.Title),
		LastValidated: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		MatcherVersion: appVersion,
		SearchStrategy: "manual_animeav1_slug",
	}
	entry.MALSeen, entry.MALStatus = animeState(anime)
	a.cachePut(entry)
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "reverse_manual_match_saved", "media_id": match.ID, "animeav1_slug": slug, "animeav1_title": match.Title, "mal_id": anime.ID, "mal_title": anime.Title})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "entry": entry, "slug": slug, "animeav1_title": match.Title})
}'''
if not pattern.search(r):
    raise SystemExit('reverseManualMatchAPI function not found')
r = pattern.sub(replacement, r, count=1)
# reverse_runtime.go already imports net/url; add errors for validation helpers.
r = r.replace('"encoding/json"\n', '"encoding/json"\n\t"errors"\n', 1)
reverse.write_text(r, encoding='utf-8')

Path('VERSION').write_text('1.7.0-rc4\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8').replace('ovelayos/animeav1-mal-sync:1.7.0-rc3', 'ovelayos/animeav1-mal-sync:1.7.0-rc4')
compose.write_text(c, encoding='utf-8')
