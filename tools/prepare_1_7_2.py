from pathlib import Path

p = Path('main.go')
s = p.read_text(encoding='utf-8')
s = s.replace('appVersion = "1.6.1"', 'appVersion = "1.7.2"', 1)

anchor = '''type Settings struct {
\tCookie          string `json:"cookie"`
\tIntervalMinutes int    `json:"interval_minutes"`
\tDryRun          bool   `json:"dry_run"`
\tOnlyIncrease    bool   `json:"only_increase"`
\tAutoSync        bool   `json:"auto_sync"`
}'''
replacement = '''type Settings struct {
\tCookie          string `json:"cookie"`
\tIntervalMinutes int    `json:"interval_minutes"`
\tDryRun          bool   `json:"dry_run"`
\tOnlyIncrease    bool   `json:"only_increase"`
\tAutoSync        bool   `json:"auto_sync"`
\tMALClientID     string `json:"mal_client_id,omitempty"`
\tMALClientSecret string `json:"mal_client_secret,omitempty"`
\tMALRedirectURI  string `json:"mal_redirect_uri,omitempty"`
}'''
if anchor not in s:
    raise SystemExit('Settings anchor not found')
s = s.replace(anchor, replacement, 1)
s = s.replace('\tapp.load()\n\tapp.loadCache()\n', '\tapp.load()\n\tapp.importMALConfigFromEnv()\n\tapp.loadCache()\n', 1)
s = s.replace('\tmux.HandleFunc("/settings", app.saveSettings)\n', '\tmux.HandleFunc("/settings", app.saveSettings)\n\tmux.HandleFunc("/mal/settings", app.saveMALSettings)\n', 1)
needle = '''\tif st.Token.AccessToken != "" {
\t\tmalStatus = "✅ Autorizado"
\t\tif st.MALUsername != "" {
\t\t\tmalStatus += " como " + html.EscapeString(st.MALUsername)
\t\t}
\t}
'''
if needle not in s:
    raise SystemExit('MAL status anchor not found')
s = s.replace(needle, needle + '\tmalConfigPanel := a.malConfigPanel(r, st, malStatus)\n', 1)
old_card = '<div class="card"><h2>MyAnimeList</h2><p>%s</p><a class="btn" href="/oauth/start">Conectar con MAL</a> <a class="btn danger" href="/oauth/disconnect">Desconectar</a></div>'
if old_card not in s:
    raise SystemExit('MAL dashboard card anchor not found')
s = s.replace(old_card, '%s', 1)
s = s.replace('html.EscapeString(st.Settings.Cookie), malStatus, st.Settings.IntervalMinutes', 'html.EscapeString(st.Settings.Cookie), malConfigPanel, st.Settings.IntervalMinutes', 1)
old = '''func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
\tcid := os.Getenv("MAL_CLIENT_ID")
\tred := os.Getenv("MAL_REDIRECT_URI")
\tif cid == "" || red == "" {
\t\thttp.Error(w, "Faltan MAL_CLIENT_ID o MAL_REDIRECT_URI", 500)
\t\treturn
\t}
'''
new = '''func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
\tcid, _, red := a.malCredentials()
\tif cid == "" || red == "" {
\t\thttp.Error(w, "Configura primero MyAnimeList desde la interfaz", http.StatusPreconditionRequired)
\t\treturn
\t}
'''
if old not in s:
    raise SystemExit('oauthStart anchor not found')
s = s.replace(old, new, 1)
old_vals = '''\tvals := url.Values{"client_id": {os.Getenv("MAL_CLIENT_ID")}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {os.Getenv("MAL_REDIRECT_URI")}, "code_verifier": {verifier}}
\tif sec := os.Getenv("MAL_CLIENT_SECRET"); sec != "" {
\t\tvals.Set("client_secret", sec)
\t}
'''
new_vals = '''\tcid, sec, red := a.malCredentials()
\tif cid == "" || red == "" {
\t\thttp.Error(w, "Configuración de MyAnimeList incompleta", http.StatusPreconditionRequired)
\t\treturn
\t}
\tvals := url.Values{"client_id": {cid}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {red}, "code_verifier": {verifier}}
\tif sec != "" {
\t\tvals.Set("client_secret", sec)
\t}
'''
if old_vals not in s:
    raise SystemExit('oauthCallback credentials anchor not found')
s = s.replace(old_vals, new_vals, 1)
old_refresh = '''\tvals := url.Values{"client_id": {os.Getenv("MAL_CLIENT_ID")}, "grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}}
\tif sec := os.Getenv("MAL_CLIENT_SECRET"); sec != "" {
\t\tvals.Set("client_secret", sec)
\t}
'''
new_refresh = '''\tcid, sec, _ := a.malCredentials()
\tif cid == "" {
\t\treturn errors.New("falta la configuración de MyAnimeList")
\t}
\tvals := url.Values{"client_id": {cid}, "grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}}
\tif sec != "" {
\t\tvals.Set("client_secret", sec)
\t}
'''
if old_refresh not in s:
    raise SystemExit('refresh credentials anchor not found')
s = s.replace(old_refresh, new_refresh, 1)
p.write_text(s, encoding='utf-8')

Path('mal_config.go').write_text(r'''package main

import (
    "fmt"
    "html"
    "net/http"
    "os"
    "strings"
)

func (a *App) malCredentials() (string, string, string) {
    a.mu.Lock()
    cid := strings.TrimSpace(a.state.Settings.MALClientID)
    sec := strings.TrimSpace(a.state.Settings.MALClientSecret)
    red := strings.TrimSpace(a.state.Settings.MALRedirectURI)
    a.mu.Unlock()
    if cid == "" { cid = strings.TrimSpace(os.Getenv("MAL_CLIENT_ID")) }
    if sec == "" { sec = strings.TrimSpace(os.Getenv("MAL_CLIENT_SECRET")) }
    if red == "" { red = strings.TrimSpace(os.Getenv("MAL_REDIRECT_URI")) }
    return cid, sec, red
}

func (a *App) importMALConfigFromEnv() {
    a.mu.Lock()
    changed := false
    if strings.TrimSpace(a.state.Settings.MALClientID) == "" {
        if v := strings.TrimSpace(os.Getenv("MAL_CLIENT_ID")); v != "" { a.state.Settings.MALClientID = v; changed = true }
    }
    if strings.TrimSpace(a.state.Settings.MALClientSecret) == "" {
        if v := strings.TrimSpace(os.Getenv("MAL_CLIENT_SECRET")); v != "" { a.state.Settings.MALClientSecret = v; changed = true }
    }
    if strings.TrimSpace(a.state.Settings.MALRedirectURI) == "" {
        if v := strings.TrimSpace(os.Getenv("MAL_REDIRECT_URI")); v != "" { a.state.Settings.MALRedirectURI = v; changed = true }
    }
    a.mu.Unlock()
    if changed { a.save() }
}

func (a *App) malConfigPanel(r *http.Request, st State, status string) string {
    cid := strings.TrimSpace(st.Settings.MALClientID)
    red := strings.TrimSpace(st.Settings.MALRedirectURI)
    configured := cid != "" && red != ""
    if red == "" {
        scheme := "http"
        if r.TLS != nil { scheme = "https" }
        red = scheme + "://" + r.Host + "/oauth/callback"
    }
    action := ""
    if configured {
        action = `<a class="btn" href="/oauth/start">Conectar con MAL</a> <a class="btn danger" href="/oauth/disconnect">Desconectar</a>`
    } else {
        status = "⚠️ Configura primero la aplicación de MyAnimeList"
    }
    secretHint := "Opcional"
    if st.Settings.MALClientSecret != "" { secretHint = "Guardado; déjalo vacío para conservarlo" }
    return fmt.Sprintf(`<div class="card"><h2>MyAnimeList</h2><p>%s</p>
<form method="post" action="/mal/settings">
<label>Client ID</label><input name="mal_client_id" value="%s" autocomplete="off" required>
<label>Client Secret</label><input type="password" name="mal_client_secret" value="" autocomplete="new-password" placeholder="%s">
<label>Redirect URI</label><input name="mal_redirect_uri" value="%s" autocomplete="off" required>
<button>Guardar configuración MAL</button> %s
</form><p class="muted">Estos datos se guardan en /data/config/config.json y no necesitan estar en el YAML de Portainer.</p></div>`,
        html.EscapeString(status), html.EscapeString(cid), html.EscapeString(secretHint), html.EscapeString(red), action)
}

func (a *App) saveMALSettings(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { http.Error(w, "POST", http.StatusMethodNotAllowed); return }
    if err := r.ParseForm(); err != nil { http.Error(w, err.Error(), http.StatusBadRequest); return }
    cid := strings.TrimSpace(r.FormValue("mal_client_id"))
    sec := strings.TrimSpace(r.FormValue("mal_client_secret"))
    red := strings.TrimSpace(r.FormValue("mal_redirect_uri"))
    if cid == "" || red == "" { http.Error(w, "Client ID y Redirect URI son obligatorios", http.StatusBadRequest); return }
    if !strings.HasPrefix(red, "http://") && !strings.HasPrefix(red, "https://") { http.Error(w, "Redirect URI debe empezar por http:// o https://", http.StatusBadRequest); return }
    a.mu.Lock()
    a.state.Settings.MALClientID = cid
    if sec != "" { a.state.Settings.MALClientSecret = sec }
    a.state.Settings.MALRedirectURI = red
    a.save()
    a.mu.Unlock()
    redirectHome(w, r)
}
''', encoding='utf-8')

Path('VERSION').write_text('1.7.2\n', encoding='utf-8')
Path('docker-compose.portainer.yml').write_text('''version: "3.8"\n\nservices:\n  animeav1-mal-sync:\n    image: ovelayos/animeav1-mal-sync:1.7.2\n    container_name: animeav1-mal-sync\n    restart: unless-stopped\n\n    ports:\n      - "8787:8787"\n\n    environment:\n      SYNC_INTERVAL_MINUTES: "60"\n      DRY_RUN: "true"\n      ONLY_INCREASE: "true"\n      AUTO_SYNC: "false"\n\n      TITLE_MATCH_THRESHOLD: "80"\n      REVERSE_TITLE_MATCH_THRESHOLD: "92"\n\n      DATA_DIR: "/data"\n      LISTEN_ADDR: ":8787"\n      LOG_TIMEZONE: "Europe/Madrid"\n      CACHE_REVALIDATE_HOURS: "24"\n\n    volumes:\n      - animeav1-mal-sync-data:/data\n\n    security_opt:\n      - seccomp=unconfined\n\nvolumes:\n  animeav1-mal-sync-data:\n    name: animeav1-mal-sync-data\n''', encoding='utf-8')

d = Path('Dockerfile')
ds = d.read_text(encoding='utf-8')
ds = ds.replace('COPY main.go ./', 'COPY *.go ./')
d.write_text(ds, encoding='utf-8')
