from pathlib import Path
import sys

branch = sys.argv[1] if len(sys.argv) > 1 else 'main'
if branch == 'main':
    old_version = '1.7.0'
    new_version = '1.7.1'
else:
    old_version = '1.8.0-rc1'
    new_version = '1.8.0-rc2'

p = Path('main.go')
s = p.read_text(encoding='utf-8')

# Version is injected at build time on current branches; keep any fallback/version literal aligned when present.
s = s.replace(f'appVersion = "{old_version}"', f'appVersion = "{new_version}"', 1)

# Carry the AnimeAV1 slug in run results and persistent cache so UI links never need a server-side authenticated lookup.
if 'SourceSlug  string   `json:"source_slug,omitempty"`' not in s:
    s = s.replace('SourceTitle string   `json:"source_title"`\n', 'SourceTitle string   `json:"source_title"`\n\tSourceSlug  string   `json:"source_slug,omitempty"`\n', 1)
if 'SourceSlug     string   `json:"source_slug,omitempty"`' not in s:
    s = s.replace('SourceTitle    string   `json:"source_title"`\n', 'SourceTitle    string   `json:"source_title"`\n\tSourceSlug     string   `json:"source_slug,omitempty"`\n', 1)

# Every normal direct-sync RunItem built from AVItem now includes its slug.
s = s.replace('RunItem{MediaID: it.MediaID, SourceTitle: it.Title,', 'RunItem{MediaID: it.MediaID, SourceTitle: it.Title, SourceSlug: it.Slug,')

# Cache entries created from a current AVItem also remember the slug.
s = s.replace('SourceTitle: normalize(it.Title), SourceSeen:', 'SourceTitle: normalize(it.Title), SourceSlug: it.Slug, SourceSeen:')
s = s.replace('SourceTitle:    normalize(it.Title),\n', 'SourceTitle:    normalize(it.Title),\n\t\t\tSourceSlug:     it.Slug,\n')
s = s.replace('SourceTitle: normalize(source.Title), SourceSeen:', 'SourceTitle: normalize(source.Title), SourceSlug: source.Slug, SourceSeen:')

# Direct browser link: never call /animeav1/open and never reuse the app's copied AnimeAV1 cookie.
old_js = '''function animeAV1IDLink(id){return '<a class="id-link" target="_blank" rel="noopener" href="/animeav1/open?media_id='+encodeURIComponent(id)+'" title="Abrir ficha en AnimeAV1">'+esc(id)+'</a>'}'''
new_js = '''function animeAV1IDLink(id,slug){const s=String(slug||'').trim();if(!s)return esc(id);return '<a class="id-link" target="_blank" rel="noopener noreferrer" href="https://animeav1.com/media/'+encodeURIComponent(s)+'" title="Abrir ficha en AnimeAV1">'+esc(id)+'</a>'}'''
if old_js not in s:
    raise SystemExit('animeAV1IDLink anchor not found')
s = s.replace(old_js, new_js, 1)
s = s.replace('animeAV1IDLink(i.media_id)', 'animeAV1IDLink(i.media_id,i.source_slug)')

# Remove the obsolete authenticated redirect endpoint and its route.
s = s.replace('\tmux.HandleFunc("/animeav1/open", app.openAnimeAV1)\n', '')
start = s.find('func (a *App) openAnimeAV1(w http.ResponseWriter, r *http.Request) {')
if start >= 0:
    end = s.find('\nfunc (a *App) deleteCacheEntryAPI', start)
    if end < 0:
        raise SystemExit('openAnimeAV1 function end not found')
    s = s[:start] + s[end+1:]

if 'SourceSlug     string   `json:"source_slug,omitempty"`' not in s:
    raise SystemExit('CacheEntry SourceSlug field was not added')

p.write_text(s, encoding='utf-8')

Path('VERSION').write_text(new_version + '\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8')
if old_version not in c:
    raise SystemExit(f'compose version anchor {old_version} not found')
c = c.replace(f'ovelayos/animeav1-mal-sync:{old_version}', f'ovelayos/animeav1-mal-sync:{new_version}', 1)
compose.write_text(c, encoding='utf-8')
