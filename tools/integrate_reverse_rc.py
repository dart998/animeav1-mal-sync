from pathlib import Path

p = Path('main.go')
s = p.read_text(encoding='utf-8')

s = s.replace('appVersion = "1.6.1"', 'appVersion = "1.7.0-rc1"')

route_anchor = '\tmux.HandleFunc("/sync", app.syncHandler)\n'
route_block = route_anchor + '\tmux.HandleFunc("/sync/reverse", app.reverseSyncHandler)\n\tmux.HandleFunc("/api/reverse/conflicts", app.reverseConflictsAPI)\n\tmux.HandleFunc("/api/reverse/resolve", app.reverseResolveAPI)\n'
if '/sync/reverse' not in s:
    if route_anchor not in s:
        raise SystemExit('route anchor not found')
    s = s.replace(route_anchor, route_block, 1)

button_old = '<button>Guardar ajustes</button> <button formaction="/sync">Sincronizar ahora</button>'
button_new = '<button>Guardar ajustes</button> <button formaction="/sync">AnimeAV1 → MAL</button> <button formaction="/sync/reverse" class="secondary">MAL → AnimeAV1</button>'
if button_old in s:
    s = s.replace(button_old, button_new, 1)
elif button_new not in s:
    raise SystemExit('sync buttons anchor not found')

js_anchor = "function openResults(kind){const items=lastData?.last?.items||[];"
reverse_js = r'''function reverseConflictBox(i){return '<div class="manual-match"><div class="warn" style="margin:8px 0">El progreso no se compara automáticamente porque AnimeAV1 puede contar especiales de forma diferente.</div><div style="display:flex;gap:8px;flex-wrap:wrap"><button type="button" class="secondary reverse-resolve" data-source="animeav1" data-media-id="'+esc(i.media_id)+'" data-mal-id="'+esc(i.mal_id)+'" data-av-title="'+esc(i.source_title)+'" data-mal-title="'+esc(i.mal_title||'')+'" data-av-seen="'+esc(i.from)+'" data-mal-seen="'+esc(i.to)+'">Usar AnimeAV1 ('+esc(i.from)+')</button><button type="button" class="reverse-resolve" data-source="mal" data-media-id="'+esc(i.media_id)+'" data-mal-id="'+esc(i.mal_id)+'" data-av-title="'+esc(i.source_title)+'" data-mal-title="'+esc(i.mal_title||'')+'" data-av-seen="'+esc(i.from)+'" data-mal-seen="'+esc(i.to)+'">Usar MAL ('+esc(i.to)+')</button></div><div class="manual-result muted"></div></div>'}
async function resolveReverseConflict(b){const box=b.closest('.manual-match'),out=box.querySelector('.manual-result');b.disabled=true;out.textContent='Guardando decisión…';try{const body=new URLSearchParams({media_id:b.dataset.mediaId,mal_id:b.dataset.malId,preferred_source:b.dataset.source,animeav1_title:b.dataset.avTitle,mal_title:b.dataset.malTitle,animeav1_seen:b.dataset.avSeen,mal_seen:b.dataset.malSeen});const r=await fetch('/api/reverse/resolve',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo guardar');out.textContent='✓ Guardado como fuente de verdad: '+(b.dataset.source==='mal'?'MyAnimeList':'AnimeAV1')+'. No volverá a preguntarse por esta pareja.';box.querySelectorAll('button').forEach(x=>x.disabled=true);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}
function bindReverseConflicts(){document.querySelectorAll('.reverse-resolve').forEach(b=>b.addEventListener('click',()=>resolveReverseConflict(b)))}
'''
if 'function reverseConflictBox(i)' not in s:
    if js_anchor not in s:
        raise SystemExit('JS anchor not found')
    s = s.replace(js_anchor, reverse_js + js_anchor, 1)

manual_old = "const manual=i.result==='error'?manualMatchBox(i):'';"
manual_new = "const manual=i.result==='error'?(String(i.message||'').startsWith('Conflicto de episodios')?reverseConflictBox(i):manualMatchBox(i)):'';"
if manual_old in s:
    s = s.replace(manual_old, manual_new, 1)
elif manual_new not in s:
    raise SystemExit('manual error box anchor not found')

bind_old = "if(kind==='error'||kind==='all')bindManualMatches()}"
bind_new = "if(kind==='error'||kind==='all'){bindManualMatches();bindReverseConflicts()}}"
if bind_old in s:
    s = s.replace(bind_old, bind_new, 1)
elif bind_new not in s:
    raise SystemExit('bind anchor not found')

p.write_text(s, encoding='utf-8')

Path('VERSION').write_text('1.7.0-rc1\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8').replace('ovelayos/animeav1-mal-sync:1.6.1', 'ovelayos/animeav1-mal-sync:1.7.0-rc1')
compose.write_text(c, encoding='utf-8')
