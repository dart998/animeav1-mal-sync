package main

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
	if cid == "" {
		cid = strings.TrimSpace(os.Getenv("MAL_CLIENT_ID"))
	}
	if sec == "" {
		sec = strings.TrimSpace(os.Getenv("MAL_CLIENT_SECRET"))
	}
	if red == "" {
		red = strings.TrimSpace(os.Getenv("MAL_REDIRECT_URI"))
	}
	return cid, sec, red
}

func (a *App) importMALConfigFromEnv() {
	a.mu.Lock()
	changed := false
	if strings.TrimSpace(a.state.Settings.MALClientID) == "" {
		if v := strings.TrimSpace(os.Getenv("MAL_CLIENT_ID")); v != "" {
			a.state.Settings.MALClientID = v
			changed = true
		}
	}
	if strings.TrimSpace(a.state.Settings.MALClientSecret) == "" {
		if v := strings.TrimSpace(os.Getenv("MAL_CLIENT_SECRET")); v != "" {
			a.state.Settings.MALClientSecret = v
			changed = true
		}
	}
	if strings.TrimSpace(a.state.Settings.MALRedirectURI) == "" {
		if v := strings.TrimSpace(os.Getenv("MAL_REDIRECT_URI")); v != "" {
			a.state.Settings.MALRedirectURI = v
			changed = true
		}
	}
	a.mu.Unlock()
	if changed {
		a.save()
	}
}

func (a *App) malConfigPanel(r *http.Request, st State, status string) string {
	cid := strings.TrimSpace(st.Settings.MALClientID)
	red := strings.TrimSpace(st.Settings.MALRedirectURI)
	configured := cid != "" && red != ""
	if red == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		red = scheme + "://" + r.Host + "/oauth/callback"
	}
	action := ""
	if configured {
		action = `<a class="btn" href="/oauth/start">Conectar con MAL</a> <a class="btn danger" href="/oauth/disconnect">Desconectar</a>`
	} else {
		status = "⚠️ Configura primero la aplicación de MyAnimeList"
	}
	secretHint := "Opcional"
	if st.Settings.MALClientSecret != "" {
		secretHint = "Guardado; déjalo vacío para conservarlo"
	}
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
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cid := strings.TrimSpace(r.FormValue("mal_client_id"))
	sec := strings.TrimSpace(r.FormValue("mal_client_secret"))
	red := strings.TrimSpace(r.FormValue("mal_redirect_uri"))
	if cid == "" || red == "" {
		http.Error(w, "Client ID y Redirect URI son obligatorios", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(red, "http://") && !strings.HasPrefix(red, "https://") {
		http.Error(w, "Redirect URI debe empezar por http:// o https://", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	a.state.Settings.MALClientID = cid
	if sec != "" {
		a.state.Settings.MALClientSecret = sec
	}
	a.state.Settings.MALRedirectURI = red
	a.save()
	a.mu.Unlock()
	redirectHome(w, r)
}
