from pathlib import Path

p = Path('main.go')
s = p.read_text(encoding='utf-8')

needle = '''\tif st.AnimeOK {\n\t\tcookieStatus = "✅ " + html.EscapeString(st.AnimeMessage)\n\t}\n\tmalStatus := "❌ No autorizado"\n'''
repl = '''\tif st.AnimeOK {\n\t\tcookieStatus = "✅ " + html.EscapeString(st.AnimeMessage)\n\t}\n\tanimeConfigPanel := a.animeAV1ConfigPanel(st, cookieStatus)\n\tmalStatus := "❌ No autorizado"\n'''
if needle not in s:
    raise SystemExit('anime status anchor not found')
s = s.replace(needle, repl, 1)

old_card = '<div class="card"><h2>AnimeAV1</h2><p>%s</p><form method="post" action="/cookie"><label>Cookie completa del navegador</label><textarea name="cookie" rows="3" placeholder="session=...; otra_cookie=...">%s</textarea><button>Guardar cookie</button> <a class="btn secondary" href="/check">Verificar</a></form></div>'
if old_card not in s:
    raise SystemExit('anime card anchor not found')
s = s.replace(old_card, '%s', 1)

old_args = 'appVersion, cookieStatus, html.EscapeString(st.Settings.Cookie), malConfigPanel, st.Settings.IntervalMinutes'
new_args = 'appVersion, animeConfigPanel, malConfigPanel, st.Settings.IntervalMinutes'
if old_args not in s:
    raise SystemExit('dashboard fmt args anchor not found')
s = s.replace(old_args, new_args, 1)
p.write_text(s, encoding='utf-8')

Path('ui_panels.go').write_text(r'''package main

import (
    "fmt"
    "html"
    "strings"
)

func (a *App) animeAV1ConfigPanel(st State, status string) string {
    cookie := strings.TrimSpace(st.Settings.Cookie)
    form := fmt.Sprintf(`<form method="post" action="/cookie">
<label>Cookie completa del navegador</label><textarea name="cookie" rows="3" placeholder="session=...; otra_cookie=...">%s</textarea>
<button>Guardar cookie</button> <a class="btn secondary" href="/check">Verificar</a></form>`, html.EscapeString(cookie))

    if st.AnimeOK && cookie != "" {
        return fmt.Sprintf(`<div class="card"><h2>AnimeAV1</h2><p>%s</p>
<details><summary class="muted" style="cursor:pointer">Cambiar cookie</summary><div style="margin-top:12px">%s</div></details></div>`, status, form)
    }
    return fmt.Sprintf(`<div class="card"><h2>AnimeAV1</h2><p>%s</p>%s</div>`, status, form)
}
''', encoding='utf-8')

mp = Path('mal_config.go')
ms = mp.read_text(encoding='utf-8')
start = ms.index('func (a *App) malConfigPanel(')
end = ms.index('func (a *App) saveMALSettings(', start)
new_panel = r'''func (a *App) malConfigPanel(r *http.Request, st State, status string) string {
    cid := strings.TrimSpace(st.Settings.MALClientID)
    red := strings.TrimSpace(st.Settings.MALRedirectURI)
    configured := cid != "" && red != ""
    authorized := configured && st.Token.AccessToken != "" && st.MALUsername != ""
    if red == "" {
        scheme := "http"
        if r.TLS != nil {
            scheme = "https"
        }
        red = scheme + "://" + r.Host + "/oauth/callback"
    }

    secretHint := "Opcional"
    if st.Settings.MALClientSecret != "" {
        secretHint = "Guardado; déjalo vacío para conservarlo"
    }
    form := fmt.Sprintf(`<form method="post" action="/mal/settings">
<label>Client ID</label><input name="mal_client_id" value="%s" autocomplete="off" required>
<label>Client Secret</label><input type="password" name="mal_client_secret" value="" autocomplete="new-password" placeholder="%s">
<label>Redirect URI</label><input name="mal_redirect_uri" value="%s" autocomplete="off" required>
<button>Guardar configuración MAL</button></form>`, html.EscapeString(cid), html.EscapeString(secretHint), html.EscapeString(red))

    if authorized {
        return fmt.Sprintf(`<div class="card"><h2>MyAnimeList</h2><p>%s</p>
<a class="btn danger" href="/oauth/disconnect">Desconectar</a>
<details style="margin-top:14px"><summary class="muted" style="cursor:pointer">Cambiar configuración MAL</summary><div style="margin-top:12px">%s</div></details>
<p class="muted">La configuración está guardada en /data/config/config.json.</p></div>`, html.EscapeString(status), form)
    }

    if !configured {
        status = "⚠️ Configura primero la aplicación de MyAnimeList"
        return fmt.Sprintf(`<div class="card"><h2>MyAnimeList</h2><p>%s</p>%s
<p class="muted">Estos datos se guardan en /data/config/config.json y no necesitan estar en el YAML de Portainer.</p></div>`, html.EscapeString(status), form)
    }

    return fmt.Sprintf(`<div class="card"><h2>MyAnimeList</h2><p>%s</p>%s
<a class="btn" href="/oauth/start">Conectar con MAL</a> <a class="btn danger" href="/oauth/disconnect">Desconectar</a>
<p class="muted">Estos datos se guardan en /data/config/config.json y no necesitan estar en el YAML de Portainer.</p></div>`, html.EscapeString(status), form)
}

'''
ms = ms[:start] + new_panel + ms[end:]

# If MAL application credentials are changed, force a fresh OAuth authorization.
old = '''\ta.mu.Lock()\n\ta.state.Settings.MALClientID = cid\n\tif sec != "" {\n\t\ta.state.Settings.MALClientSecret = sec\n\t}\n\ta.state.Settings.MALRedirectURI = red\n\ta.save()\n\ta.mu.Unlock()\n'''
new = '''\ta.mu.Lock()\n\tchanged := cid != a.state.Settings.MALClientID || red != a.state.Settings.MALRedirectURI || (sec != "" && sec != a.state.Settings.MALClientSecret)\n\ta.state.Settings.MALClientID = cid\n\tif sec != "" {\n\t\ta.state.Settings.MALClientSecret = sec\n\t}\n\ta.state.Settings.MALRedirectURI = red\n\tif changed {\n\t\ta.state.Token = Token{}\n\t\ta.state.MALUsername = ""\n\t}\n\ta.save()\n\ta.mu.Unlock()\n'''
if old not in ms:
    raise SystemExit('save MAL settings anchor not found')
ms = ms.replace(old, new, 1)
mp.write_text(ms, encoding='utf-8')

Path('VERSION').write_text('1.7.3\n', encoding='utf-8')
cp = Path('docker-compose.portainer.yml')
cs = cp.read_text(encoding='utf-8').replace('ovelayos/animeav1-mal-sync:1.7.2', 'ovelayos/animeav1-mal-sync:1.7.3')
cp.write_text(cs, encoding='utf-8')
