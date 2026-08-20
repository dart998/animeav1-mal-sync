package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const (
	appVersion = "1.7.0-rc7"
	authURL    = "https://myanimelist.net/v1/oauth2/authorize"
	tokenURL   = "https://myanimelist.net/v1/oauth2/token"
	apiBase    = "https://api.myanimelist.net/v2"
)

type Token struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ObtainedAt   int64  `json:"obtained_at"`
}

type Settings struct {
	Cookie          string `json:"cookie"`
	IntervalMinutes int    `json:"interval_minutes"`
	DryRun          bool   `json:"dry_run"`
	OnlyIncrease    bool   `json:"only_increase"`
	AutoSync        bool   `json:"auto_sync"`
}

type IDString string

func (id *IDString) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*id = ""
		return nil
	}
	if strings.HasPrefix(raw, "\"") {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = IDString(value)
		return nil
	}
	*id = IDString(raw)
	return nil
}

type RunItem struct {
	MediaID     IDString `json:"media_id"`
	SourceTitle string   `json:"source_title"`
	MALID       int      `json:"mal_id,omitempty"`
	MALTitle    string   `json:"mal_title,omitempty"`
	MALID2      int      `json:"mal_id_2,omitempty"`
	MALTitle2   string   `json:"mal_title_2,omitempty"`
	MatchScore  int      `json:"match_score,omitempty"`
	From        int      `json:"from,omitempty"`
	To          int      `json:"to,omitempty"`
	Status      string   `json:"status"`
	Result      string   `json:"result"`
	Message     string   `json:"message,omitempty"`
	Direction   string   `json:"direction,omitempty"`
	ErrorType   string   `json:"error_type,omitempty"`
}

type LastRun struct {
	Status    string    `json:"status"`
	Started   int64     `json:"started"`
	Finished  int64     `json:"finished"`
	Found     int       `json:"found"`
	Updated   int       `json:"updated"`
	Skipped   int       `json:"skipped"`
	Errors    int       `json:"errors"`
	Message   string    `json:"message"`
	Unmatched []string  `json:"unmatched"`
	Items     []RunItem `json:"items,omitempty"`
}

type State struct {
	Settings     Settings `json:"settings"`
	Token        Token    `json:"token"`
	OAuthState   string   `json:"oauth_state"`
	CodeVerifier string   `json:"code_verifier"`
	Last         LastRun  `json:"last"`
	AnimeOK      bool     `json:"anime_ok"`
	AnimeMessage string   `json:"anime_message"`
	MALUsername  string   `json:"mal_username"`
}

type App struct {
	mu                sync.Mutex
	cacheMu           sync.Mutex
	state             State
	running           bool
	progressProcessed int
	progressTotal     int
	progressMessage   string
	progressTrigger   string
	cancelSync        context.CancelFunc
	cache             map[string]CacheEntry
	dataDir           string
	client            *http.Client
}

type CacheEntry struct {
	MediaID        IDString `json:"media_id"`
	MALID          int      `json:"mal_id"`
	MALTitle       string   `json:"mal_title"`
	MALID2         int      `json:"mal_id_2,omitempty"`
	MALTitle2      string   `json:"mal_title_2,omitempty"`
	MAL2Episodes   int      `json:"mal_2_episodes,omitempty"`
	MAL2Seen       int      `json:"mal_2_seen,omitempty"`
	MAL2Status     string   `json:"mal_2_status,omitempty"`
	MatchType      string   `json:"match_type,omitempty"`
	MatchScore     int      `json:"match_score"`
	SourceTitle    string   `json:"source_title"`
	SourceSeen     int      `json:"source_seen"`
	SourceStatus   int      `json:"source_status"`
	SourceTotal    int      `json:"source_total"`
	MALSeen        int      `json:"mal_seen"`
	MALStatus      string   `json:"mal_status"`
	LastValidated  int64    `json:"last_validated"`
	UpdatedAt      int64    `json:"updated_at"`
	MatcherVersion string   `json:"matcher_version,omitempty"`
	SearchStrategy string   `json:"search_strategy,omitempty"`
	NegativeUntil  int64    `json:"negative_until,omitempty"`
	NegativeReason string   `json:"negative_reason,omitempty"`
}

type AVItem struct {
	MediaID  IDString          `json:"media_id"`
	Title    string            `json:"title"`
	Aliases  map[string]string `json:"aliases"`
	Seen     int               `json:"seen"`
	Total    int               `json:"total"`
	Status   int               `json:"status"`
	Score    int               `json:"score"`
	Favorite bool              `json:"favorite"`
	Slug     string            `json:"slug"`
}

type MALSearch struct {
	Data []struct {
		Node struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"node"`
	} `json:"data"`
}

type MALAnime struct {
	ID                int    `json:"id"`
	Title             string `json:"title"`
	NumEpisodes       int    `json:"num_episodes"`
	MediaType         string `json:"media_type"`
	StartDate         string `json:"start_date"`
	AlternativeTitles struct {
		Synonyms []string `json:"synonyms"`
		English  string   `json:"en"`
		Japanese string   `json:"ja"`
	} `json:"alternative_titles"`
	MyListStatus *struct {
		Status             string `json:"status"`
		NumEpisodesWatched int    `json:"num_episodes_watched"`
	} `json:"my_list_status"`
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func getenvInt(k string, d int) int {
	v, err := strconv.Atoi(os.Getenv(k))
	if err == nil && v > 0 {
		return v
	}
	return d
}
func getenvBool(k string, d bool) bool {
	v := strings.ToLower(os.Getenv(k))
	if v == "true" || v == "1" || v == "yes" {
		return true
	}
	if v == "false" || v == "0" || v == "no" {
		return false
	}
	return d
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8787/health")
		if err != nil {
			os.Exit(1)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			os.Exit(1)
		}
		return
	}

	app := &App{
		dataDir: getenv("DATA_DIR", "/data"),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
	if err := os.MkdirAll(app.dataDir, 0755); err != nil {
		log.Fatal(err)
	}
	app.ensureDefaultAliases()
	app.load()
	app.loadCache()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.dashboard)
	mux.HandleFunc("/favicon.svg", favicon)
	mux.HandleFunc("/health", app.healthCheckHTTP)
	mux.HandleFunc("/api/status", app.health)
	mux.HandleFunc("/api/logs", app.logsAPI)
	mux.HandleFunc("/log", app.history)
	mux.HandleFunc("/cookie", app.saveCookie)
	mux.HandleFunc("/settings", app.saveSettings)
	mux.HandleFunc("/check", app.check)
	mux.HandleFunc("/test", app.check)
	mux.HandleFunc("/sync", app.syncHandler)
	mux.HandleFunc("/sync/reverse", app.reverseSyncHandler)
	mux.HandleFunc("/api/reverse/conflicts", app.reverseConflictsAPI)
	mux.HandleFunc("/api/reverse/resolve", app.reverseResolveAPI)
	mux.HandleFunc("/api/reverse/manual-match", app.reverseManualMatchAPI)
	mux.HandleFunc("/sync/stop", app.stopSyncHandler)
	mux.HandleFunc("/cache/clear", app.clearCacheHandler)
	mux.HandleFunc("/api/cache", app.cacheAPI)
	mux.HandleFunc("/api/cache/delete", app.deleteCacheEntryAPI)
	mux.HandleFunc("/api/cache/manual", app.manualCacheEntryAPI)
	mux.HandleFunc("/api/cache/candidates", app.cacheCandidatesAPI)
	mux.HandleFunc("/api/cache/recompute", app.recomputeCacheEntryAPI)
	mux.HandleFunc("/animeav1/open", app.openAnimeAV1)
	mux.HandleFunc("/history/clear", app.clearHistoryHandler)
	mux.HandleFunc("/oauth/start", app.oauthStart)
	mux.HandleFunc("/oauth/callback", app.oauthCallback)
	mux.HandleFunc("/oauth/disconnect", app.oauthDisconnect)
	mux.HandleFunc("/history", app.history)
	mux.HandleFunc("/history/raw", app.historyRaw)

	go app.scheduler()
	addr := getenv("LISTEN_ADDR", ":8787")
	log.Printf("AnimeAV1 MAL Sync escuchando en %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (a *App) healthCheckHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"ok","version":%q}`, appVersion)
}

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	io.WriteString(w, `<svg width="512" height="512" viewBox="0 0 512 512" fill="none" xmlns="http://www.w3.org/2000/svg">
    <path d="M8 41.5L103.447 206.531H179.723L122.482 107.593H275.17L160.688 305.603L198.893 371.562L389.518 41.5H8Z" fill="#000"/>
    <path d="M389.518 107.593H465.794L256 470.5L217.795 404.541L389.518 107.593Z" fill="#000"/>
    <path d="M504 41.5L465.795 107.459L427.723 41.5H504Z" fill="#000"/>
    <style>
        path { fill: #00A691; }
        @media (prefers-color-scheme: dark) {
            path { fill: #3CECD6; }
        }
    </style>
</svg>`)
}

func (a *App) load() {
	a.state.Settings.IntervalMinutes = getenvInt("SYNC_INTERVAL_MINUTES", 60)
	a.state.Settings.DryRun = getenvBool("DRY_RUN", true)
	a.state.Settings.OnlyIncrease = getenvBool("ONLY_INCREASE", true)
	a.state.Settings.AutoSync = getenvBool("AUTO_SYNC", false)
	b, err := os.ReadFile(a.configPath())
	if err != nil {
		// Migración transparente desde versiones anteriores.
		b, err = os.ReadFile(filepath.Join(a.dataDir, "state.json"))
	}
	if err == nil {
		_ = json.Unmarshal(b, &a.state)
	}
	if a.state.Settings.IntervalMinutes <= 0 {
		a.state.Settings.IntervalMinutes = 60
	}
}
func (a *App) configPath() string {
	return filepath.Join(a.dataDir, "config", "config.json")
}
func (a *App) save() {
	b, _ := json.MarshalIndent(a.state, "", "  ")
	_ = os.MkdirAll(filepath.Dir(a.configPath()), 0700)
	_ = os.WriteFile(a.configPath(), b, 0600)
}
func (a *App) appendHistory(v any) {
	b, _ := json.Marshal(v)
	f, err := os.OpenFile(filepath.Join(a.dataDir, "history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		defer f.Close()
		f.Write(append(b, '\n'))
	}
}

func (a *App) ensureDefaultAliases() {
	path := filepath.Join(a.dataDir, "aliases.json")
	// Los aliases forman parte de la lógica del matcher. Antes de 1.5.3 solo se
	// copiaban en una instalación nueva, por lo que los volúmenes persistentes no
	// recibían las correcciones incluidas en versiones posteriores. Ahora se
	// fusionan sin borrar ni reemplazar aliases personalizados del usuario.
	defaults := map[string][]string{
		"Temple":                    {"TenPuru", "TenPuru: No One Can Live on Loneliness"},
		"Mahou Shoujo ni Akogarete": {"Gushing over Magical Girls", "Looking Up to Magical Girls"},
		"Futoku no Guild":           {"Immoral Guild"},
		"Mayo Chiki!":               {"Mayo Chiki"},
		"Seikon no Qwaser":          {"The Qwaser of Stigmata", "Seikon no Quasar"},
		"Mushoku Tensei II: Isekai Ittara Honki Dasu": {"Mushoku Tensei: Jobless Reincarnation Season 2", "Mushoku Tensei 2nd Season"},
		"Mato Seihei no Slave 2":                      {"Mato Seihei no Slave 2nd Season", "Chained Soldier Season 2"},
		"Sawaranaide Kotesashi-kun":                   {"Don't Touch Kotesashi", "Hands Off: Sawaranaide Kotesashi-kun"},
		"Ichijouma Mankitsugurashi!":                  {"Ichijouma Mankitsugurashi"},
		"S-Rank Monster no \"Behemoth\" dakedo, Neko to Machigawarete Elf Musume no Pet toshite Kurashitemasu": {"Beheneko: The Elf-Girl's Cat is Secretly an S-Ranked Monster!", "Beheneko", "Behemoth S-Ranked Monster"},
		"S-Rank Monster no Behemoth dakedo, Neko to Machigawarete Elf Musume no Pet toshite Kurashitemasu":     {"Beheneko: The Elf-Girl's Cat is Secretly an S-Ranked Monster!", "Beheneko", "Behemoth S-Ranked Monster"},
	}
	existing := map[string][]string{}
	if b, err := os.ReadFile(path); err == nil {
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) == nil {
			for k, v := range raw {
				var simple []string
				if json.Unmarshal(v, &simple) == nil {
					existing[k] = append(existing[k], simple...)
					continue
				}
				var rich struct {
					Search    []string `json:"search"`
					Preferred string   `json:"preferred"`
				}
				if json.Unmarshal(v, &rich) == nil {
					if rich.Preferred != "" {
						existing[k] = append(existing[k], rich.Preferred)
					}
					existing[k] = append(existing[k], rich.Search...)
				}
			}
		}
	}
	for k, values := range defaults {
		seen := map[string]bool{}
		for _, v := range existing[k] {
			seen[normalize(v)] = true
		}
		for _, v := range values {
			if n := normalize(v); n != "" && !seen[n] {
				existing[k] = append(existing[k], v)
				seen[n] = true
			}
		}
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	_ = os.WriteFile(path, append(b, '\n'), 0600)
}

func (a *App) cachePath() string {
	return filepath.Join(a.dataDir, "cache.json")
}

func (a *App) loadCache() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.cache = map[string]CacheEntry{}
	b, err := os.ReadFile(a.cachePath())
	if err == nil {
		_ = json.Unmarshal(b, &a.cache)
	}
	if a.cache == nil {
		a.cache = map[string]CacheEntry{}
	}
}

func (a *App) saveCacheLocked() error {
	b, err := json.MarshalIndent(a.cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.cachePath() + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.cachePath())
}

func (a *App) cacheGet(mediaID IDString) (CacheEntry, bool) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	v, ok := a.cache[string(mediaID)]
	return v, ok
}

func (a *App) cachePut(v CacheEntry) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if a.cache == nil {
		a.cache = map[string]CacheEntry{}
	}
	a.cache[string(v.MediaID)] = v
	_ = a.saveCacheLocked()
}

func (a *App) cacheDelete(mediaID IDString) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	delete(a.cache, string(mediaID))
	_ = a.saveCacheLocked()
}

func (a *App) cacheCount() int {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	return len(a.cache)
}

func (a *App) scheduler() {
	for {
		a.mu.Lock()
		mins := a.state.Settings.IntervalMinutes
		last := a.state.Last.Finished
		running := a.running
		auto := a.state.Settings.AutoSync
		a.mu.Unlock()
		if mins < 1 {
			mins = 60
		}
		if auto && !running && (last == 0 || time.Now().Unix()-last >= int64(mins*60)) {
			go a.runSync("scheduled")
		}
		time.Sleep(30 * time.Second)
	}
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	st := a.state
	running := a.running
	processed := a.progressProcessed
	total := a.progressTotal
	progressMessage := a.progressMessage
	progressTrigger := a.progressTrigger
	a.mu.Unlock()
	cookieStatus := "❌ Sin configurar"
	if st.Settings.Cookie != "" {
		cookieStatus = "⚠️ Guardada, sin verificar"
		if st.AnimeMessage != "" && st.AnimeMessage != "Pendiente de verificar" {
			cookieStatus = "❌ " + html.EscapeString(st.AnimeMessage)
		}
	}
	if st.AnimeOK {
		cookieStatus = "✅ " + html.EscapeString(st.AnimeMessage)
	}
	malStatus := "❌ No autorizado"
	if st.Token.AccessToken != "" {
		malStatus = "✅ Autorizado"
		if st.MALUsername != "" {
			malStatus += " como " + html.EscapeString(st.MALUsername)
		}
	}
	lastStatus := st.Last.Status
	if lastStatus == "" {
		lastStatus = "Nunca"
	}
	runText := "No"
	if running {
		runText = "Sí"
	}
	page := fmt.Sprintf(`<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>AnimeAV1 → MAL</title><link rel="icon" type="image/svg+xml" href="/favicon.svg"><style>
body{font-family:Arial,sans-serif;background:#111827;color:#e5e7eb;max-width:1000px;margin:30px auto;padding:0 16px}h1{margin-bottom:8px}.card{background:#1f2937;border-radius:12px;padding:20px;margin:16px 0}input,textarea{width:100%%;box-sizing:border-box;background:#111827;color:#fff;border:1px solid #4b5563;border-radius:8px;padding:10px;margin:6px 0 12px}button,.btn{display:inline-block;background:#14b8a6;color:#041311;border:0;border-radius:8px;padding:10px 15px;font-weight:bold;text-decoration:none;cursor:pointer}.secondary{background:#374151;color:#fff}.danger{background:#ef4444;color:#fff}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:10px}.stat{background:#111827;padding:12px;border-radius:8px}.stat.clickable{cursor:pointer}.stat.clickable:hover{outline:1px solid #14b8a6}.muted{color:#9ca3af}.id-link{color:#9ca3af;text-decoration:none}.id-link:hover{color:#d1d5db;text-decoration:underline}.msg{white-space:pre-wrap;word-break:break-word}.progress-wrap{display:none;margin-top:16px}.progress-track{height:22px;background:#111827;border:1px solid #4b5563;border-radius:999px;overflow:hidden}.progress-bar{height:100%%;width:0;background:#14b8a6;transition:width .25s}.progress-label{margin-top:7px;color:#d1d5db}.modal{display:none;position:fixed;inset:0;background:#000b;z-index:20;padding:4vh 3vw}.modal.open{display:block}.modal-box{background:#1f2937;max-width:1100px;max-height:88vh;margin:auto;border-radius:12px;padding:18px;overflow:auto}.modal-head{display:flex;justify-content:space-between;align-items:center;gap:15px}.table-wrap{overflow:auto}table{width:100%%;border-collapse:collapse;font-size:14px}th,td{padding:9px;border-bottom:1px solid #374151;text-align:left;vertical-align:top}th{position:sticky;top:0;background:#1f2937}.ok{color:#6ee7b7}.bad{color:#fca5a5}.warn{color:#fde68a}</style></head><body>
<h1>AnimeAV1 → MyAnimeList</h1><div class="muted">v%s · EX4100 ARMv7 · lectura SvelteKit por HTTP</div>
<div class="card"><h2>AnimeAV1</h2><p>%s</p><form method="post" action="/cookie"><label>Cookie completa del navegador</label><textarea name="cookie" rows="3" placeholder="session=...; otra_cookie=...">%s</textarea><button>Guardar cookie</button> <a class="btn secondary" href="/check">Verificar</a></form></div>
<div class="card"><h2>MyAnimeList</h2><p>%s</p><a class="btn" href="/oauth/start">Conectar con MAL</a> <a class="btn danger" href="/oauth/disconnect">Desconectar</a></div>
<div class="card"><h2>Sincronización</h2><form method="post" action="/settings"><label>Intervalo en minutos</label><input type="number" min="1" name="interval" value="%d"><label><input style="width:auto" type="checkbox" name="dry" %s> Modo simulación (no escribe cambios)</label><br><label><input style="width:auto" type="checkbox" name="increase" %s> Solo aumentar episodios</label><br><label><input style="width:auto" type="checkbox" name="auto" %s> Sincronización automática</label><br><br><button>Guardar ajustes</button> <button formaction="/sync">AnimeAV1 → MAL</button> <button formaction="/sync/reverse" class="secondary">MAL → AnimeAV1</button></form><form method="post" action="/sync/stop" style="display:inline"><button id="stopButton" class="danger" style="display:none">Detener sincronización</button></form> <button class="secondary" onclick="openCache()">Ver caché</button> <form method="post" action="/cache/clear" style="display:inline" onsubmit="return confirm('¿Eliminar toda la caché?')"><button id="clearCacheButton" class="secondary">Eliminar caché</button></form><p class="muted">Caché persistente: <b id="cacheCount">%d</b> coincidencias.</p><div id="progressWrap" class="progress-wrap"><div class="progress-track"><div id="progressBar" class="progress-bar"></div></div><div id="progressLabel" class="progress-label"></div></div></div>
<div class="card"><h2>Estado</h2><div class="grid"><div class="stat"><b>Ejecutándose</b><br><span id="runningText">%s</span></div><div class="stat"><b>Último estado</b><br><span id="lastStatus">%s</span></div><div class="stat clickable" onclick="openResults('all')"><b>Encontrados</b><br><span id="found">%d</span></div><div class="stat clickable" onclick="openResults('updated')"><b>Actualizados</b><br><span id="updated">%d</span></div><div class="stat clickable" onclick="openResults('error')"><b>Errores</b><br><span id="errors">%d</span></div></div><p id="lastMessage" class="msg">%s</p><p><a class="btn secondary" target="_blank" rel="noopener" href="/api/status">JSON</a> <a class="btn secondary" target="_blank" rel="noopener" href="/log">Logs</a></p></div>
<div id="modal" class="modal" onclick="if(event.target===this)closeModal()"><div class="modal-box"><div class="modal-head"><h2 id="modalTitle">Detalles</h2><button class="secondary" onclick="closeModal()">Cerrar</button></div><div id="modalBody"></div></div></div>
<script>
let lastData=null; const initial={running:%t,processed:%d,total:%d,message:%q,trigger:%q};
function esc(v){return String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function updateProgress(x){const manual=!!x.running;progressWrap.style.display=manual?'block':'none';stopButton.style.display=manual?'inline-block':'none';clearCacheButton.disabled=!!x.running;if(manual){const pct=x.progress_total?Math.min(100,Math.floor(x.progress_processed*100/x.progress_total)):0;progressBar.style.width=pct+'%%';progressLabel.textContent=(x.progress_message||'Sincronizando')+' · '+x.progress_processed+'/'+x.progress_total+' ('+pct+'%%)'}}
async function pollStatus(){try{const r=await fetch('/api/status',{cache:'no-store'});const x=await r.json();lastData=x;runningText.textContent=x.running?'Sí':'No';lastStatus.textContent=x.last_status||'Nunca';found.textContent=x.last?.found??0;updated.textContent=x.last?.updated??0;errors.textContent=x.last?.errors??0;lastMessage.textContent=x.last?.message||'';cacheCount.textContent=x.cache_entries??0;updateProgress(x)}catch(e){}}
function showModal(title,html){modalTitle.textContent=title;modalBody.innerHTML=html;modal.classList.add('open')} function closeModal(){modal.classList.remove('open')}
function manualMatchBox(i){const q=encodeURIComponent(i.source_title||'');return '<div class="manual-match"><div style="margin:8px 0 10px"><a class="btn secondary" target="_blank" rel="noopener" href="https://myanimelist.net/anime.php?q='+q+'">🔎 Buscar «'+esc(i.source_title)+'» en MyAnimeList ↗</a></div><div style="display:grid;grid-template-columns:1fr 1fr auto;gap:8px;align-items:end"><label>MAL ID<input class="manual-mal-1" type="number" min="1" inputmode="numeric" placeholder="Obligatorio"></label><label>MAL ID 2<input class="manual-mal-2" type="number" min="1" inputmode="numeric" placeholder="Opcional, temporada dividida"></label><button type="button" class="manual-save" data-media-id="'+esc(i.media_id)+'">Guardar</button></div><div class="manual-result muted"></div></div>'}
function reverseManualMatchBox(i){const q=encodeURIComponent(i.mal_title||i.source_title||'');return '<div class="manual-match reverse-manual-match"><div class="warn" style="margin:8px 0">No se ha podido identificar automáticamente la ficha de AnimeAV1. El MAL ID ya es conocido: #'+esc(i.mal_id)+'.</div><div style="margin:8px 0 10px"><a class="btn secondary" target="_blank" rel="noopener" href="https://animeav1.com/catalogo" title="Abrir el catálogo de AnimeAV1">🔎 Buscar en AnimeAV1 ↗</a> <span class="muted">Busca: '+esc(i.mal_title||i.source_title||'')+'</span></div><div style="display:grid;grid-template-columns:1fr auto;gap:8px;align-items:end"><label>URL o slug de AnimeAV1<input class="manual-av1-ref" type="text" placeholder="https://animeav1.com/media/... o slug"></label><button type="button" class="reverse-manual-save" data-mal-id="'+esc(i.mal_id)+'" data-mal-title="'+esc(i.mal_title||i.source_title||'')+'">Guardar</button></div><div class="manual-result muted"></div></div>'}
function animeAV1IDLink(id){return '<a class="id-link" target="_blank" rel="noopener" href="/animeav1/open?media_id='+encodeURIComponent(id)+'" title="Abrir ficha en AnimeAV1">'+esc(id)+'</a>'}
function malIDLink(id){return '<a class="id-link" target="_blank" rel="noopener" href="https://myanimelist.net/anime/'+encodeURIComponent(id)+'" title="Abrir ficha en MyAnimeList">#'+esc(id)+'</a>'}
function resultTable(items,cacheMode=false){if(!items.length)return '<p>Sin elementos.</p>';const actionHead=cacheMode?'<th aria-label="Acciones"></th>':'';return '<div class="table-wrap"><table><thead><tr><th>AnimeAV1</th><th>MAL</th><th>Puntos</th><th>Episodios</th><th>Resultado</th><th>Detalle</th>'+actionHead+'</tr></thead><tbody>'+items.map(i=>{const action=cacheMode?'<td style="white-space:nowrap"><button type="button" class="secondary inspect-button" data-media-id="'+esc(i.media_id)+'" title="Ver candidatos">🔍</button> <button type="button" class="secondary recompute-button" data-media-id="'+esc(i.media_id)+'" title="Recalcular coincidencia">↻</button> <button type="button" class="danger trash-button" data-media-id="'+esc(i.media_id)+'" title="Eliminar esta coincidencia de la caché">🗑️</button></td>':'';const reverseUnmatched=i.result==='error'&&i.direction==='reverse'&&i.error_type==='animeav1_unmatched';const manual=i.result==='error'?(String(i.message||'').startsWith('Conflicto de episodios')?reverseConflictBox(i):(reverseUnmatched?reverseManualMatchBox(i):manualMatchBox(i))):'';return '<tr data-cache-row="'+esc(i.media_id)+'"><td>'+(i.media_id?(esc(i.source_title)+'<br>'+animeAV1IDLink(i.media_id)):'—<br><span class="muted">No identificado</span>')+'</td><td>'+esc(i.mal_title||'—')+(i.mal_id?' '+malIDLink(i.mal_id):'')+(i.mal_title_2?'<br>↳ '+esc(i.mal_title_2)+(i.mal_id_2?' '+malIDLink(i.mal_id_2):''):'')+'</td><td>'+esc(i.match_score||'—')+'</td><td>'+esc(i.from)+' → '+esc(i.to)+'</td><td>'+esc(i.result)+'</td><td>'+esc(i.message||'')+manual+'</td>'+action+'</tr>'}).join('')+'</tbody></table></div>'}
function bindReverseManualMatches(){document.querySelectorAll('.reverse-manual-save').forEach(b=>b.addEventListener('click',async()=>{const box=b.closest('.reverse-manual-match'),row=b.closest('tr'),ref=box.querySelector('.manual-av1-ref').value.trim(),out=box.querySelector('.manual-result');if(!ref){out.textContent='Introduce la URL o el slug de AnimeAV1.';return}b.disabled=true;out.textContent='Resolviendo ficha y guardando…';try{const body=new URLSearchParams({animeav1_ref:ref,mal_id:b.dataset.malId,mal_title:b.dataset.malTitle});const r=await fetch('/api/reverse/manual-match',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo guardar');out.textContent='✓ Relación guardada con '+(x.entry?.source_title||'AnimeAV1')+' (ID '+(x.entry?.media_id||'')+').';if(lastData?.last?.items)lastData.last.items=lastData.last.items.filter(i=>!(i.result==='error'&&String(i.mal_id)===String(b.dataset.malId)&&!i.media_id));setTimeout(()=>row?.remove(),350);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}))}
function bindManualMatches(){document.querySelectorAll('.manual-save').forEach(b=>b.addEventListener('click',async()=>{const box=b.closest('.manual-match'),row=b.closest('tr'),one=box.querySelector('.manual-mal-1').value.trim(),two=box.querySelector('.manual-mal-2').value.trim(),out=box.querySelector('.manual-result');if(!one){out.textContent='Introduce al menos el primer ID de MAL.';return}b.disabled=true;out.textContent='Validando y guardando…';try{const body=new URLSearchParams({media_id:b.dataset.mediaId,mal_id:one});if(two)body.set('mal_id_2',two);const r=await fetch('/api/cache/manual',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok)throw new Error(x.error||'No se pudo guardar');out.textContent=two?'✓ Guardado como temporada dividida.':'✓ Coincidencia guardada.';if(lastData?.last?.items)lastData.last.items=lastData.last.items.filter(i=>!(String(i.media_id)===String(b.dataset.mediaId)&&i.result==='error'));if(lastData?.last){lastData.last.errors=x.errors??Math.max(0,(lastData.last.errors||0)-1);lastData.last.status=x.status||lastData.last.status;lastData.last.message=x.message||lastData.last.message}errors.textContent=x.errors??0;lastStatus.textContent=x.status||lastStatus.textContent;lastMessage.textContent=x.message||lastMessage.textContent;setTimeout(()=>{row?.remove();const tbody=document.querySelector('#modalBody tbody');if(tbody&&!tbody.children.length)modalBody.innerHTML='<p class="ok">✓ No hay errores de matching pendientes.</p>'},350);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}))}
function reverseConflictBox(i){return '<div class="manual-match"><div class="warn" style="margin:8px 0">El progreso no se compara automáticamente porque AnimeAV1 puede contar especiales de forma diferente.</div><div style="display:flex;gap:8px;flex-wrap:wrap"><button type="button" class="secondary reverse-resolve" data-source="animeav1" data-media-id="'+esc(i.media_id)+'" data-mal-id="'+esc(i.mal_id)+'" data-av-title="'+esc(i.source_title)+'" data-mal-title="'+esc(i.mal_title||'')+'" data-av-seen="'+esc(i.from)+'" data-mal-seen="'+esc(i.to)+'">Usar AnimeAV1 ('+esc(i.from)+')</button><button type="button" class="reverse-resolve" data-source="mal" data-media-id="'+esc(i.media_id)+'" data-mal-id="'+esc(i.mal_id)+'" data-av-title="'+esc(i.source_title)+'" data-mal-title="'+esc(i.mal_title||'')+'" data-av-seen="'+esc(i.from)+'" data-mal-seen="'+esc(i.to)+'">Usar MAL ('+esc(i.to)+')</button></div><div class="manual-result muted"></div></div>'}
async function resolveReverseConflict(b){const box=b.closest('.manual-match'),out=box.querySelector('.manual-result');b.disabled=true;out.textContent='Guardando decisión…';try{const body=new URLSearchParams({media_id:b.dataset.mediaId,mal_id:b.dataset.malId,preferred_source:b.dataset.source,animeav1_title:b.dataset.avTitle,mal_title:b.dataset.malTitle,animeav1_seen:b.dataset.avSeen,mal_seen:b.dataset.malSeen});const r=await fetch('/api/reverse/resolve',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});const x=await r.json().catch(()=>({}));if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo guardar');out.textContent='✓ Guardado como fuente de verdad: '+(b.dataset.source==='mal'?'MyAnimeList':'AnimeAV1')+'. No volverá a preguntarse por esta pareja.';box.querySelectorAll('button').forEach(x=>x.disabled=true);await pollStatus()}catch(e){out.textContent=e.message||'No se pudo guardar';b.disabled=false}}
function bindReverseConflicts(){document.querySelectorAll('.reverse-resolve').forEach(b=>b.addEventListener('click',()=>resolveReverseConflict(b)))}
function openResults(kind){const items=lastData?.last?.items||[];const filtered=kind==='all'?items:items.filter(i=>i.result===kind);showModal(kind==='all'?'Coincidencias de la última ejecución':kind==='updated'?'Actualizados':'Errores',resultTable(filtered));if(kind==='error'||kind==='all'){bindManualMatches();bindReverseManualMatches();bindReverseConflicts()}}
async function deleteCacheMatch(mediaID,title){if(!confirm('¿Eliminar de la caché la coincidencia de "'+title+'"? En la siguiente sincronización se buscará de nuevo.'))return;try{const r=await fetch('/api/cache/delete',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'media_id='+encodeURIComponent(mediaID)});const x=await r.json().catch(()=>({}));if(!r.ok)throw new Error(x.error||'No se pudo eliminar');await openCache();await pollStatus()}catch(e){alert(e.message||'No se pudo eliminar la coincidencia')}}
async function inspectCacheMatch(mediaID){showModal('Candidatos','<p>Buscando candidatos en MAL…</p>');try{const r=await fetch('/api/cache/candidates?media_id='+encodeURIComponent(mediaID),{cache:'no-store'});const x=await r.json();if(!r.ok)throw new Error(x.error||'No se pudo consultar');const rows=(x.items||[]).map(i=>'<tr><td>'+esc(i.mal_title)+' <span class="muted">#'+i.mal_id+'</span></td><td>'+esc(i.score)+'</td><td>'+esc(i.episodes||'—')+'</td><td>'+esc(i.media_type||'—')+'</td><td class="'+(i.accepted?'ok':'bad')+'">'+esc(i.reason)+'</td></tr>').join('');showModal('Candidatos: '+x.source_title,'<div class="table-wrap"><table><thead><tr><th>MAL</th><th>Puntos</th><th>Episodios</th><th>Tipo</th><th>Diagnóstico</th></tr></thead><tbody>'+rows+'</tbody></table></div>')}catch(e){showModal('Candidatos','<p>'+esc(e.message)+'</p>')}}
async function recomputeCacheMatch(mediaID){if(!confirm('¿Recalcular esta coincidencia ahora?'))return;try{const r=await fetch('/api/cache/recompute',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'media_id='+encodeURIComponent(mediaID)});const x=await r.json();if(!r.ok||!x.ok)throw new Error(x.error||'No se pudo recalcular');await openCache();await pollStatus()}catch(e){alert(e.message)}}
async function openCache(){try{const r=await fetch('/api/cache',{cache:'no-store'});const x=await r.json();const items=(x.items||[]).map(i=>({media_id:i.media_id,source_title:i.source_title,mal_title:i.mal_title,mal_id:i.mal_id,mal_title_2:i.mal_title_2,mal_id_2:i.mal_id_2,match_score:i.match_score,from:i.mal_seen,to:i.source_seen,result:'cache',message:'Validado: '+(i.last_validated?new Date(i.last_validated*1000).toLocaleString():'—')}));showModal('Caché ('+items.length+')',resultTable(items,true));document.querySelectorAll('.trash-button').forEach(b=>b.addEventListener('click',()=>{const row=b.closest('tr');deleteCacheMatch(b.dataset.mediaId,row?.children[0]?.textContent||'esta entrada')}));document.querySelectorAll('.inspect-button').forEach(b=>b.addEventListener('click',()=>inspectCacheMatch(b.dataset.mediaId)));document.querySelectorAll('.recompute-button').forEach(b=>b.addEventListener('click',()=>recomputeCacheMatch(b.dataset.mediaId)))}catch(e){showModal('Caché','<p>Error al cargar la caché.</p>')}}
updateProgress({running:initial.running,progress_processed:initial.processed,progress_total:initial.total,progress_message:initial.message,progress_trigger:initial.trigger});setInterval(pollStatus,1000);pollStatus();
</script></body></html>`, appVersion, cookieStatus, html.EscapeString(st.Settings.Cookie), malStatus, st.Settings.IntervalMinutes, checked(st.Settings.DryRun), checked(st.Settings.OnlyIncrease), checked(st.Settings.AutoSync), a.cacheCount(), runText, html.EscapeString(lastStatus), st.Last.Found, st.Last.Updated, st.Last.Errors, html.EscapeString(st.Last.Message), running, processed, total, progressMessage, progressTrigger)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, page)
}

func checked(v bool) string {
	if v {
		return "checked"
	}
	return ""
}
func redirectHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) saveCookie(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	r.ParseForm()
	c := strings.TrimSpace(r.FormValue("cookie"))
	a.mu.Lock()
	a.state.Settings.Cookie = c
	a.state.AnimeOK = false
	a.state.AnimeMessage = "Pendiente de verificar"
	a.save()
	a.mu.Unlock()
	redirectHome(w, r)
}
func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	r.ParseForm()
	n, _ := strconv.Atoi(r.FormValue("interval"))
	if n < 1 {
		n = 60
	}
	a.mu.Lock()
	a.state.Settings.IntervalMinutes = n
	a.state.Settings.DryRun = r.FormValue("dry") != ""
	a.state.Settings.OnlyIncrease = r.FormValue("increase") != ""
	a.state.Settings.AutoSync = r.FormValue("auto") != ""
	a.save()
	a.mu.Unlock()
	redirectHome(w, r)
}

func (a *App) check(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cookie := a.state.Settings.Cookie
	a.mu.Unlock()
	msg, err := a.verifyAnime(cookie)
	a.mu.Lock()
	a.state.AnimeOK = err == nil
	if err != nil {
		a.state.AnimeMessage = err.Error()
	} else {
		a.state.AnimeMessage = msg
	}
	a.save()
	a.mu.Unlock()
	redirectHome(w, r)
}

func (a *App) verifyAnime(cookie string) (string, error) {
	items, err := a.scrape(cookie)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Sesión válida · %d entradas leídas", len(items)), nil
}

func browserHeaders(req *http.Request, cookie string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/537.36 Chrome/124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	req.Header.Set("Cookie", cookie)
}

var (
	reSpaces      = regexp.MustCompile(`\s+`)
	reIntField    = func(name string) *regexp.Regexp { return regexp.MustCompile(name + `\s*:\s*(-?\d+|null)`) }
	reBoolField   = func(name string) *regexp.Regexp { return regexp.MustCompile(name + `\s*:\s*(true|false)`) }
	reStringField = func(name string) *regexp.Regexp { return regexp.MustCompile(name + `\s*:\s*("(?:\\.|[^"\\])*")`) }
	reAlias       = regexp.MustCompile(`("(?:\\.|[^"\\])*")\s*:\s*("(?:\\.|[^"\\])*")`)
)

func normalize(s string) string {
	s = strings.ToLower(html.UnescapeString(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(reSpaces.ReplaceAllString(b.String(), " "))
}
func jsString(v string) string {
	if v == "" {
		return ""
	}
	x, err := strconv.Unquote(v)
	if err != nil {
		return strings.Trim(v, `"`)
	}
	return x
}
func fieldInt(block, name string) int {
	m := reIntField(name).FindStringSubmatch(block)
	if len(m) < 2 || m[1] == "null" {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
func fieldBool(block, name string) bool {
	m := reBoolField(name).FindStringSubmatch(block)
	return len(m) > 1 && m[1] == "true"
}
func fieldID(block, name string) IDString {
	re := regexp.MustCompile(`(?:"?` + regexp.QuoteMeta(name) + `"?)\s*:\s*(?:"([^"\\]*(?:\\.[^"\\]*)*)"|(-?[0-9]+))`)
	m := re.FindStringSubmatch(block)
	if len(m) < 3 {
		return ""
	}
	if m[1] != "" {
		return IDString(jsString(m[1]))
	}
	return IDString(m[2])
}
func fieldString(block, name string) string {
	m := reStringField(name).FindStringSubmatch(block)
	if len(m) < 2 {
		return ""
	}
	return jsString(m[1])
}
func balancedValue(src string, start int, open, close byte) (string, error) {
	if start < 0 || start >= len(src) || src[start] != open {
		return "", errors.New("inicio de bloque inválido")
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(src); i++ {
		c := src[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c == open {
			depth++
		}
		if c == close {
			depth--
			if depth == 0 {
				return src[start : i+1], nil
			}
		}
	}
	return "", errors.New("bloque SvelteKit incompleto")
}
func splitTopObjects(array string) []string {
	out := []string{}
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i := 0; i < len(array); i++ {
		c := array[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, array[start:i+1])
				start = -1
			}
		}
	}
	return out
}
func extractObject(block, key string) string {
	i := strings.Index(block, key+":")
	if i < 0 {
		return ""
	}
	j := strings.Index(block[i:], "{")
	if j < 0 {
		return ""
	}
	v, _ := balancedValue(block, i+j, '{', '}')
	return v
}
func parseLibraryEntries(body string) ([]AVItem, error) {
	i := strings.Index(body, "libraryEntries:")
	if i < 0 {
		return nil, errors.New("AnimeAV1 no contiene libraryEntries; cookie caducada o formato cambiado")
	}
	j := strings.Index(body[i:], "[")
	if j < 0 {
		return nil, errors.New("libraryEntries sin array")
	}
	arr, err := balancedValue(body, i+j, '[', ']')
	if err != nil {
		return nil, err
	}
	objects := splitTopObjects(arr)
	items := make([]AVItem, 0, len(objects))
	for _, obj := range objects {
		media := extractObject(obj, "media")
		if media == "" {
			continue
		}
		it := AVItem{MediaID: fieldID(obj, "mediaId"), Status: fieldInt(obj, "status"), Seen: fieldInt(obj, "episode"), Score: fieldInt(obj, "score"), Favorite: fieldBool(obj, "favorite"), Title: fieldString(media, "title"), Total: fieldInt(media, "episodesCount"), Slug: fieldString(media, "slug"), Aliases: map[string]string{}}
		aka := extractObject(media, "aka")
		for _, m := range reAlias.FindAllStringSubmatch(aka, -1) {
			if len(m) == 3 {
				it.Aliases[jsString(m[1])] = jsString(m[2])
			}
		}
		if it.Title != "" {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil, errors.New("libraryEntries encontrado pero no se pudo leer ninguna entrada")
	}
	return items, nil
}
func (a *App) scrape(cookie string) ([]AVItem, error) {
	return a.scrapeContext(context.Background(), cookie)
}

func (a *App) scrapeContext(ctx context.Context, cookie string) ([]AVItem, error) {
	if strings.TrimSpace(cookie) == "" {
		return nil, errors.New("falta la cookie de AnimeAV1")
	}
	u := getenv("ANIMEAV1_LIBRARY_URL", "https://animeav1.com/cuenta/listas")
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	browserHeaders(req, cookie)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("AnimeAV1 respondió HTTP %d", resp.StatusCode)
	}
	low := strings.ToLower(string(b))
	if strings.Contains(low, "verifique que es un ser humano") || strings.Contains(low, "iniciar sesión") && !strings.Contains(string(b), "libraryEntries:") {
		return nil, errors.New("cookie caducada o sesión no válida")
	}
	return parseLibraryEntries(string(b))
}

func randomURLSafe(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(b), "=")
}
func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
	cid := os.Getenv("MAL_CLIENT_ID")
	red := os.Getenv("MAL_REDIRECT_URI")
	if cid == "" || red == "" {
		http.Error(w, "Faltan MAL_CLIENT_ID o MAL_REDIRECT_URI", 500)
		return
	}
	verifier := randomURLSafe(64)
	state := randomURLSafe(32)
	a.mu.Lock()
	a.state.CodeVerifier = verifier
	a.state.OAuthState = state
	a.save()
	a.mu.Unlock()
	q := url.Values{"response_type": {"code"}, "client_id": {cid}, "redirect_uri": {red}, "code_challenge": {verifier}, "code_challenge_method": {"plain"}, "state": {state}}
	http.Redirect(w, r, authURL+"?"+q.Encode(), http.StatusFound)
}
func (a *App) oauthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	a.mu.Lock()
	expected := a.state.OAuthState
	verifier := a.state.CodeVerifier
	a.mu.Unlock()
	if code == "" || state == "" || state != expected {
		http.Error(w, "Respuesta OAuth inválida", 400)
		return
	}
	vals := url.Values{"client_id": {os.Getenv("MAL_CLIENT_ID")}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {os.Getenv("MAL_REDIRECT_URI")}, "code_verifier": {verifier}}
	if sec := os.Getenv("MAL_CLIENT_SECRET"); sec != "" {
		vals.Set("client_secret", sec)
	}
	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("Token MAL HTTP %d: %s", resp.StatusCode, string(b)), 500)
		return
	}
	var t Token
	if err = json.Unmarshal(b, &t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	t.ObtainedAt = time.Now().Unix()
	a.mu.Lock()
	a.state.Token = t
	a.state.OAuthState = ""
	a.state.CodeVerifier = ""
	a.save()
	a.mu.Unlock()
	_ = a.fetchMALUser()
	redirectHome(w, r)
}
func (a *App) oauthDisconnect(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.state.Token = Token{}
	a.state.MALUsername = ""
	a.save()
	a.mu.Unlock()
	redirectHome(w, r)
}

func (a *App) refreshIfNeeded() error {
	a.mu.Lock()
	t := a.state.Token
	a.mu.Unlock()
	if t.AccessToken == "" {
		return errors.New("MyAnimeList no está autorizado")
	}
	if time.Now().Unix() < t.ObtainedAt+t.ExpiresIn-120 {
		return nil
	}
	vals := url.Values{"client_id": {os.Getenv("MAL_CLIENT_ID")}, "grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}}
	if sec := os.Getenv("MAL_CLIENT_SECRET"); sec != "" {
		vals.Set("client_secret", sec)
	}
	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("refresh MAL HTTP %d: %s", resp.StatusCode, string(b))
	}
	var nt Token
	if err = json.Unmarshal(b, &nt); err != nil {
		return err
	}
	nt.ObtainedAt = time.Now().Unix()
	a.mu.Lock()
	a.state.Token = nt
	a.save()
	a.mu.Unlock()
	return nil
}
func (a *App) malRequest(method, path string, vals url.Values, out any) error {
	return a.malRequestContext(context.Background(), method, path, vals, out)
}

func (a *App) malRequestContext(ctx context.Context, method, path string, vals url.Values, out any) error {
	if err := a.refreshIfNeeded(); err != nil {
		return err
	}
	a.mu.Lock()
	tok := a.state.Token.AccessToken
	a.mu.Unlock()

	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		var body io.Reader
		if vals != nil {
			body = strings.NewReader(vals.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if vals != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := a.client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
		} else {
			b, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode < 300 {
				if out != nil {
					return json.Unmarshal(b, out)
				}
				return nil
			} else {
				lastErr = fmt.Errorf("MAL HTTP %d: %s", resp.StatusCode, string(b))
				// Los 4xx salvo 429 son errores permanentes y no mejoran al reintentar.
				if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
					return lastErr
				}
			}
		}
		if attempt < attempts {
			wait := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return lastErr
}

func (a *App) fetchMALUser() error {
	var x struct {
		Name string `json:"name"`
	}
	if err := a.malRequest("GET", "/users/@me", nil, &x); err != nil {
		return err
	}
	a.mu.Lock()
	a.state.MALUsername = x.Name
	a.save()
	a.mu.Unlock()
	return nil
}

func similarity(a, b string) int {
	a = normalize(a)
	b = normalize(b)
	if a == b {
		return 100
	}
	ar := []rune(a)
	br := []rune(b)
	dp := make([]int, len(br)+1)
	for j := range dp {
		dp[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		prev := dp[0]
		dp[0] = i
		for j := 1; j <= len(br); j++ {
			tmp := dp[j]
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			x := dp[j] + 1
			if dp[j-1]+1 < x {
				x = dp[j-1] + 1
			}
			if prev+cost < x {
				x = prev + cost
			}
			dp[j] = x
			prev = tmp
		}
	}
	dist := dp[len(br)]
	max := len(ar)
	if len(br) > max {
		max = len(br)
	}
	if max == 0 {
		return 100
	}
	return 100 - (dist * 100 / max)
}
func (a *App) candidateTitles(it AVItem) []string {
	out := []string{it.Title}
	seen := map[string]bool{normalize(it.Title): true}
	add := func(v string) {
		n := normalize(v)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, strings.TrimSpace(v))
		}
	}
	for _, v := range it.Aliases {
		add(v)
	}

	// Compatible con el formato clásico {"Título":["alias"]} y con el formato
	// enriquecido {"Título":{"search":[...],"preferred":"..."}}.
	b, err := os.ReadFile(filepath.Join(a.dataDir, "aliases.json"))
	if err == nil {
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) == nil {
			for key, value := range raw {
				if normalize(key) != normalize(it.Title) {
					continue
				}
				var simple []string
				if json.Unmarshal(value, &simple) == nil {
					for _, v := range simple {
						add(v)
					}
					continue
				}
				var rich struct {
					Search    []string `json:"search"`
					Preferred string   `json:"preferred"`
				}
				if json.Unmarshal(value, &rich) == nil {
					add(rich.Preferred)
					for _, v := range rich.Search {
						add(v)
					}
				}
			}
		}
	}
	return out
}

func animeTitles(anime MALAnime) []string {
	out := []string{anime.Title, anime.AlternativeTitles.English, anime.AlternativeTitles.Japanese}
	out = append(out, anime.AlternativeTitles.Synonyms...)
	return out
}

func truncateRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return strings.TrimSpace(string(r[:max]))
}

func searchQueries(title string) []string {
	clean := strings.TrimSpace(title)
	clean = strings.ReplaceAll(clean, `"`, " ")
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, " ")
	punctuationFree := regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(clean, " ")
	punctuationFree = regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(punctuationFree), " ")
	base := strings.TrimSpace(baseTitleDisplay(clean))

	variants := []string{
		truncateRunes(clean, 64),
		truncateRunes(punctuationFree, 64),
		truncateRunes(base, 64),
	}
	if i := strings.IndexAny(clean, ":,-("); i >= 3 {
		variants = append(variants, truncateRunes(clean[:i], 64))
	}
	words := strings.Fields(clean)
	for _, n := range []int{10, 8, 6, 4} {
		if len(words) > n {
			variants = append(variants, truncateRunes(strings.Join(words[:n], " "), 64))
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(variants))
	for _, q := range variants {
		q = strings.TrimSpace(q)
		key := normalize(q)
		if len([]rune(key)) < 3 || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, q)
	}
	return out
}

func romanSeason(s string) int {
	switch strings.ToUpper(s) {
	case "I":
		return 1
	case "II":
		return 2
	case "III":
		return 3
	case "IV":
		return 4
	case "V":
		return 5
	case "VI":
		return 6
	case "VII":
		return 7
	case "VIII":
		return 8
	case "IX":
		return 9
	case "X":
		return 10
	}
	return 0
}

func seasonNumber(title string) int {
	n := strings.TrimSpace(strings.ToLower(title))
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bseason\s*([1-9][0-9]*)\b`),
		regexp.MustCompile(`\b([1-9][0-9]*)(?:st|nd|rd|th)\s+season\b`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(n); len(m) == 2 {
			v, _ := strconv.Atoi(m[1])
			return v
		}
	}
	// Numeral romano al final o antes de un separador.
	if m := regexp.MustCompile(`(?i)(?:^|\s)(II|III|IV|V|VI|VII|VIII|IX|X)(?:\s*[:\-]|$)`).FindStringSubmatch(title); len(m) == 2 {
		return romanSeason(m[1])
	}
	// Número arábigo final: formato habitual de secuelas en MAL/AnimeAV1.
	if m := regexp.MustCompile(`(?:^|\s)([2-9]|[1-9][0-9])\s*$`).FindStringSubmatch(n); len(m) == 2 {
		v, _ := strconv.Atoi(m[1])
		return v
	}
	return 0
}

func partNumber(title string) int {
	n := strings.ToLower(title)
	if m := regexp.MustCompile(`\bpart\s*([1-9][0-9]*)\b`).FindStringSubmatch(n); len(m) == 2 {
		v, _ := strconv.Atoi(m[1])
		return v
	}
	return 0
}

func hasSeasonMarker(title string) bool {
	n := strings.ToLower(title)
	return regexp.MustCompile(`\bseason\s*[1-9][0-9]*\b|\b[1-9][0-9]*(?:st|nd|rd|th)\s+season\b`).MatchString(n) || regexp.MustCompile(`(?i)(?:^|\s)(II|III|IV|V|VI|VII|VIII|IX|X)(?:\s*[:\-]|$)`).MatchString(title)
}

func seasonMismatch(source, candidate string) bool {
	a, b := seasonNumber(source), seasonNumber(candidate)
	return a > 0 && b > 0 && a != b
}

func baseTitleDisplay(title string) string {
	s := strings.TrimSpace(title)
	patterns := []string{
		`(?i)\bseason\s*[1-9][0-9]*\b`, `(?i)\b[1-9][0-9]*(?:st|nd|rd|th)\s+season\b`,
		`(?i)\bpart\s*[1-9][0-9]*\b`, `(?i)\b[1-9][0-9]*\s*(?:st|nd|rd|th)?\s*part\b`,
		`(?i)(?:^|\s)(II|III|IV|V|VI|VII|VIII|IX|X)(?:\s*[:\-]|$)`,
		`(?i)(?:^|\s)([2-9]|[1-9][0-9])\s*$`,
	}
	for _, pattern := range patterns {
		s = regexp.MustCompile(pattern).ReplaceAllString(s, " ")
	}
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

func baseTitle(title string) string { return normalize(baseTitleDisplay(title)) }

func isGenericTitle(title string) bool {
	n := baseTitle(title)
	if n == "" {
		return true
	}
	generic := map[string]bool{
		"season": true, "second season": true, "third season": true,
		"movie": true, "special": true, "ova": true, "ona": true,
		"part": true, "tv": true,
	}
	if generic[n] {
		return true
	}
	return len([]rune(n)) < 4
}

func tokenOverlap(a, b string) int {
	aa, bb := strings.Fields(baseTitle(a)), strings.Fields(baseTitle(b))
	if len(aa) == 0 || len(bb) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, x := range aa {
		set[x] = true
	}
	common := 0
	for _, x := range bb {
		if set[x] {
			common++
		}
	}
	den := len(aa)
	if len(bb) < den {
		den = len(bb)
	}
	return common * 100 / den
}

func variantPenalty(source, candidate string) int {
	s, c := strings.ToLower(source), strings.ToLower(candidate)
	penalty := 0
	for _, marker := range []string{"manner movie", "recap", "special", "pv", "trailer", "summary"} {
		if strings.Contains(c, marker) && !strings.Contains(s, marker) {
			penalty += 35
		}
	}
	if strings.Contains(c, "part 2") && !strings.Contains(s, "part 2") {
		penalty += 20
	}
	if strings.Contains(c, "movie") && !strings.Contains(s, "movie") && !strings.Contains(s, "gekijouban") {
		penalty += 15
	}
	return penalty
}

type titleMatch struct {
	score     int
	baseScore int
	source    string
	candidate string
}

func evaluateTitlePair(source, candidate string, primary bool) (titleMatch, bool) {
	if isGenericTitle(source) || isGenericTitle(candidate) {
		return titleMatch{}, false
	}
	// Una coincidencia exacta normalizada siempre es segura, también cuando el
	// nombre procede de un alias de AnimeAV1 o de un título alternativo de MAL.
	// Se evalúa antes de las heurísticas de temporadas para evitar falsos negativos.
	if normalize(source) == normalize(candidate) {
		return titleMatch{score: 120 - variantPenalty(source, candidate), baseScore: 100, source: source, candidate: candidate}, true
	}
	if seasonMismatch(source, candidate) {
		return titleMatch{}, false
	}
	sn, cn := seasonNumber(source), seasonNumber(candidate)
	sp, cp := partNumber(source), partNumber(candidate)
	fullExact := normalize(source) == normalize(candidate)
	// Part 2 es la segunda mitad de una temporada, no la temporada 2. Nunca se
	// intercambian de forma automática como una coincidencia individual.
	if !fullExact && hasSeasonMarker(source) && cp > 0 && sp == 0 && !hasSeasonMarker(candidate) {
		return titleMatch{}, false
	}
	if !fullExact && sp > 0 && hasSeasonMarker(candidate) && cp == 0 && !hasSeasonMarker(source) {
		return titleMatch{}, false
	}
	baseExact := baseTitle(source) == baseTitle(candidate)
	// Una secuela explícita nunca puede emparejarse con una entrada sin temporada,
	// salvo que el título completo sea idéntico.
	if !fullExact && sn > 1 && cn == 0 {
		return titleMatch{}, false
	}
	// Una entrada sin temporada explícita representa normalmente la primera serie.
	// No debe enlazarse con una secuela explícita (II, 2nd Season, etc.).
	if !fullExact && sn == 0 && cn > 1 {
		return titleMatch{}, false
	}
	if fullExact {
		return titleMatch{score: 120 - variantPenalty(source, candidate), baseScore: 100, source: source, candidate: candidate}, true
	}
	if baseExact {
		score := 108 - variantPenalty(source, candidate)
		if sn > 0 && sn == cn {
			score += 4
		}
		return titleMatch{score: score, baseScore: 100, source: source, candidate: candidate}, score >= 90
	}
	baseScore := similarity(baseTitle(source), baseTitle(candidate))
	overlap := tokenOverlap(source, candidate)
	minBase := getenvInt("BASE_TITLE_MATCH_THRESHOLD", 88)
	if !primary {
		minBase = maxInt(minBase, 94)
	}
	if baseScore < minBase || overlap < 60 {
		return titleMatch{}, false
	}
	score := baseScore - variantPenalty(source, candidate)
	if sn > 0 && sn == cn {
		score += 3
	}
	return titleMatch{score: score, baseScore: baseScore, source: source, candidate: candidate}, score >= getenvInt("TITLE_MATCH_THRESHOLD", 88)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *App) resolve(ctx context.Context, it AVItem) (MALAnime, int, error) {
	started := time.Now()
	ids := map[int]string{}
	var searchErr error
	queries := 0
	candidateTitles := a.candidateTitles(it)

	// Una sola consulta por título/alias. Se eliminan los recortes progresivos,
	// responsables de candidatos absurdos y de decenas de peticiones por anime.
	for _, title := range candidateTitles {
		if isGenericTitle(title) {
			continue
		}
		query := truncateRunes(strings.TrimSpace(strings.ReplaceAll(title, `"`, " ")), 64)
		query = regexp.MustCompile(`\s+`).ReplaceAllString(query, " ")
		if len([]rune(normalize(query))) < 3 {
			continue
		}
		queries++
		var sr MALSearch
		path := "/anime?q=" + url.QueryEscape(query) + "&limit=20"
		if err := a.malRequestContext(ctx, "GET", path, nil, &sr); err != nil {
			searchErr = err
			continue
		}
		for _, x := range sr.Data {
			ids[x.Node.ID] = x.Node.Title
			// El título principal exacto es el caso más fiable y evita descargar
			// decenas de fichas cuando un alias ya resolvió la serie.
			if normalize(x.Node.Title) == normalize(title) {
				var anime MALAnime
				fields := "id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status"
				if err := a.malRequestContext(ctx, "GET", fmt.Sprintf("/anime/%d?fields=%s", x.Node.ID, fields), nil, &anime); err == nil {
					for _, malTitle := range animeTitles(anime) {
						if m, ok := evaluateTitlePair(title, malTitle, normalize(title) == normalize(it.Title)); ok {
							a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "matcher_timing", "title": it.Title, "queries": queries, "checked": 1, "duration_ms": time.Since(started).Milliseconds(), "result": "exact", "mal_title": anime.Title})
							return anime, m.score, nil
						}
					}
				}
			}
		}
		if len(ids) >= 30 {
			break
		}
	}
	if len(ids) == 0 && searchErr != nil {
		return MALAnime{}, 0, searchErr
	}

	bestScore, bestBase, bestRank := -1, -1, -100000
	var best MALAnime
	bestRejectedScore, bestRejectedTitle := -1, ""
	checked := 0
	for id := range ids {
		if checked >= 30 {
			break
		}
		checked++
		var anime MALAnime
		fields := "id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status"
		if err := a.malRequestContext(ctx, "GET", fmt.Sprintf("/anime/%d?fields=%s", id, fields), nil, &anime); err != nil {
			continue
		}
		var tm titleMatch
		matched := false
		for _, sourceTitle := range candidateTitles {
			primary := normalize(sourceTitle) == normalize(it.Title)
			for _, malTitle := range animeTitles(anime) {
				m, ok := evaluateTitlePair(sourceTitle, malTitle, primary)
				if !primary && ok && normalize(sourceTitle) != normalize(malTitle) && baseTitle(sourceTitle) != baseTitle(malTitle) {
					ok = false
				}
				if ok && (!matched || m.score > tm.score || (m.score == tm.score && m.baseScore > tm.baseScore)) {
					tm, matched = m, true
				}
			}
		}
		if !matched {
			for _, sourceTitle := range candidateTitles {
				for _, malTitle := range animeTitles(anime) {
					score := similarity(normalize(sourceTitle), normalize(malTitle))
					if score > bestRejectedScore {
						bestRejectedScore, bestRejectedTitle = score, anime.Title
					}
				}
			}
			continue
		}
		rank := tm.score
		if it.Total > 0 && anime.NumEpisodes > 0 {
			d := int(math.Abs(float64(it.Total - anime.NumEpisodes)))
			if d == 0 {
				rank += 4
			} else if d <= 2 {
				rank++
			} else if d > 5 {
				rank -= 8
			}
		}
		if rank > bestRank || (rank == bestRank && (tm.score > bestScore || (tm.score == bestScore && tm.baseScore > bestBase))) {
			bestRank, bestScore, bestBase, best = rank, tm.score, tm.baseScore, anime
		}
		if bestScore >= 120 {
			break
		}
	}
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "matcher_timing", "title": it.Title, "queries": queries, "checked": checked, "duration_ms": time.Since(started).Milliseconds(), "result": "evaluated"})
	threshold := getenvInt("TITLE_MATCH_THRESHOLD", 88)
	if best.ID == 0 || bestScore < threshold {
		if len(ids) == 0 {
			return MALAnime{}, -1, fmt.Errorf("sin candidatos devueltos por MAL")
		}
		if bestRejectedTitle != "" {
			return MALAnime{}, bestScore, fmt.Errorf("candidatos encontrados, pero ninguno superó el umbral (%d puntos); mejor descartado: %q (%d%% de similitud bruta)", bestScore, bestRejectedTitle, bestRejectedScore)
		}
		return MALAnime{}, bestScore, fmt.Errorf("candidatos encontrados, pero ninguno superó el umbral (%d puntos)", bestScore)
	}
	return best, bestScore, nil
}

func stripPartMarker(title string) string {
	s := strings.TrimSpace(title)
	// Para detectar un cour dividido solo eliminamos el marcador de parte. No
	// eliminamos temporada, números romanos ni números finales, porque forman
	// parte de la identidad de la serie/temporada.
	patterns := []string{
		`(?i)\s*[:\-–—]?\s*\bpart\s*2\b\s*$`,
		`(?i)\s*[:\-–—]?\s*\b2(?:nd)?\s+part\b\s*$`,
	}
	for _, pattern := range patterns {
		s = regexp.MustCompile(pattern).ReplaceAllString(s, "")
	}
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

func validSplitTitlePair(firstTitle, secondTitle string) bool {
	if partNumber(secondTitle) != 2 || partNumber(firstTitle) != 0 {
		return false
	}
	stripped := stripPartMarker(secondTitle)
	if stripped == "" {
		return false
	}
	// Regla deliberadamente estricta: el título de la segunda ficha debe ser el
	// mismo título de la primera tras retirar únicamente "Part 2". Los títulos
	// alternativos se comparan entre sí en validSplitPair.
	return normalize(firstTitle) == normalize(stripped)
}

func validSplitPair(first, second MALAnime) bool {
	if first.ID == 0 || second.ID == 0 || first.ID == second.ID || second.NumEpisodes <= 0 {
		return false
	}
	if first.MediaType != "" && second.MediaType != "" && first.MediaType != second.MediaType {
		return false
	}
	for _, ft := range animeTitles(first) {
		if strings.TrimSpace(ft) == "" {
			continue
		}
		for _, st := range animeTitles(second) {
			if strings.TrimSpace(st) != "" && validSplitTitlePair(ft, st) {
				return true
			}
		}
	}
	return false
}

// resolveSplitPart2 localiza una segunda ficha MAL cuando AnimeAV1 agrupa en una
// sola entrada una temporada que MAL publicó en dos cours. No confunde Season 2
// con Part 2: la primera ficha ya debe haber sido elegida y la segunda debe llevar
// explícitamente "Part 2", compartir la misma base y completar el total esperado.
func (a *App) resolveSplitPart2(ctx context.Context, it AVItem, first MALAnime) (MALAnime, int, error) {
	if first.ID == 0 || first.NumEpisodes <= 0 || it.Total <= first.NumEpisodes {
		return MALAnime{}, 0, nil
	}
	ids := map[int]bool{}
	queries := []string{first.Title + " Part 2", baseTitleDisplay(first.Title) + " Part 2"}
	for _, t := range animeTitles(first) {
		if strings.TrimSpace(t) != "" {
			queries = append(queries, t+" Part 2")
		}
	}
	for _, q := range queries {
		var sr MALSearch
		if err := a.malRequestContext(ctx, "GET", "/anime?q="+url.QueryEscape(truncateRunes(q, 64))+"&limit=100", nil, &sr); err != nil {
			continue
		}
		for _, x := range sr.Data {
			ids[x.Node.ID] = true
		}
	}
	bestScore := -1
	var best MALAnime
	for id := range ids {
		if id == first.ID {
			continue
		}
		var anime MALAnime
		if err := a.malRequestContext(ctx, "GET", fmt.Sprintf("/anime/%d?fields=id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status", id), nil, &anime); err != nil {
			continue
		}
		if partNumber(anime.Title) != 2 || anime.NumEpisodes <= 0 {
			continue
		}
		if !validSplitPair(first, anime) {
			continue
		}
		combined := first.NumEpisodes + anime.NumEpisodes
		// Aceptamos totales iguales o una ficha aún en emisión con total desconocido
		// en AnimeAV1; nunca aceptamos una suma inferior a los episodios vistos.
		if combined < it.Seen {
			continue
		}
		diff := int(math.Abs(float64(combined - it.Total)))
		score := 110 - diff
		if combined == it.Total {
			score += 10
		}
		if score > bestScore {
			bestScore, best = score, anime
		}
	}
	if best.ID == 0 {
		return MALAnime{}, 0, nil
	}
	return best, bestScore, nil
}

func splitDesired(it AVItem, firstEpisodes, secondEpisodes int) (int, int) {
	first := desiredFor(it, firstEpisodes)
	second := it.Seen - firstEpisodes
	if second < 0 {
		second = 0
	}
	if secondEpisodes > 0 && second > secondEpisodes {
		second = secondEpisodes
	}
	return first, second
}

func splitStatus(sourceStatus string, desired, maxEpisodes int, isSecond bool) string {
	if sourceStatus == "plan_to_watch" {
		return sourceStatus
	}
	if maxEpisodes > 0 && desired >= maxEpisodes {
		return "completed"
	}
	if desired == 0 && isSecond {
		return "plan_to_watch"
	}
	return sourceStatus
}

func (a *App) updateMALItem(ctx context.Context, anime MALAnime, desired int, status string, dry, only bool) (int, string, bool, error) {
	current, currentStatus := animeState(anime)
	if only && desired < current {
		return current, currentStatus, false, nil
	}
	if desired == current && currentStatus == status {
		return current, currentStatus, false, nil
	}
	if !dry {
		vals := url.Values{"status": {status}, "num_watched_episodes": {strconv.Itoa(desired)}}
		if err := a.malRequestContext(ctx, "PUT", fmt.Sprintf("/anime/%d/my_list_status", anime.ID), vals, nil); err != nil {
			return current, currentStatus, false, err
		}
	}
	return desired, status, true, nil
}

func malStatus(s int) string {
	switch s {
	case 0:
		return "watching"
	case 1:
		return "plan_to_watch"
	case 2:
		return "completed"
	case 3:
		return "on_hold"
	case 4:
		return "dropped"
	default:
		return "plan_to_watch"
	}
}

func (a *App) syncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	r.ParseForm()
	if r.FormValue("interval") != "" {
		a.saveSettingsNoRedirect(r)
	}
	go a.runSync("manual")
	redirectHome(w, r)
}

func (a *App) stopSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	a.mu.Lock()
	cancel := a.cancelSync
	manual := a.running && a.progressTrigger == "manual"
	if manual {
		a.progressMessage = "Deteniendo sincronización…"
	}
	a.mu.Unlock()
	if manual && cancel != nil {
		cancel()
		a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "sync_stop_requested", "message": "Detención manual solicitada"})
	}
	redirectHome(w, r)
}

func (a *App) clearCacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		http.Error(w, "Detén la sincronización antes de eliminar la caché", http.StatusConflict)
		return
	}
	a.cacheMu.Lock()
	a.cache = map[string]CacheEntry{}
	err := a.saveCacheLocked()
	a.cacheMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "cache_cleared", "message": "Caché eliminada"})
	redirectHome(w, r)
}

func (a *App) saveSettingsNoRedirect(r *http.Request) {
	n, _ := strconv.Atoi(r.FormValue("interval"))
	if n < 1 {
		n = 60
	}
	a.mu.Lock()
	a.state.Settings.IntervalMinutes = n
	a.state.Settings.DryRun = r.FormValue("dry") != ""
	a.state.Settings.OnlyIncrease = r.FormValue("increase") != ""
	a.state.Settings.AutoSync = r.FormValue("auto") != ""
	a.save()
	a.mu.Unlock()
}

func sourceUnchanged(c CacheEntry, it AVItem) bool {
	return c.SourceTitle == normalize(it.Title) && c.SourceSeen == it.Seen && c.SourceStatus == it.Status && c.SourceTotal == it.Total
}

func desiredFor(it AVItem, maxEpisodes int) int {
	desired := it.Seen
	if maxEpisodes > 0 && desired > maxEpisodes {
		desired = maxEpisodes
	}
	return desired
}

func animeState(anime MALAnime) (int, string) {
	if anime.MyListStatus == nil {
		return 0, ""
	}
	return anime.MyListStatus.NumEpisodesWatched, anime.MyListStatus.Status
}

func (a *App) getCachedMAL(ctx context.Context, c CacheEntry) (MALAnime, error) {
	var anime MALAnime
	err := a.malRequestContext(ctx, "GET", fmt.Sprintf("/anime/%d?fields=id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status", c.MALID), nil, &anime)
	return anime, err
}

func (a *App) getCachedMAL2(ctx context.Context, c CacheEntry) (MALAnime, error) {
	if c.MALID2 == 0 {
		return MALAnime{}, nil
	}
	var anime MALAnime
	err := a.malRequestContext(ctx, "GET", fmt.Sprintf("/anime/%d?fields=id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status", c.MALID2), nil, &anime)
	return anime, err
}

func (a *App) runSync(trigger string) {
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
	a.progressMessage = "Preparando sincronización"
	a.progressTrigger = trigger
	cookie := a.state.Settings.Cookie
	dry := a.state.Settings.DryRun
	only := a.state.Settings.OnlyIncrease
	started := time.Now().Unix()
	a.state.Last = LastRun{Status: "running", Started: started, Message: "Sincronización " + trigger}
	a.save()
	a.mu.Unlock()
	defer cancel()

	last := LastRun{Status: "ok", Started: started}
	items, err := a.scrapeContext(ctx, cookie)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			last.Status = "cancelled"
			last.Message = "Detenida por el usuario antes de leer la biblioteca"
			a.finish(last)
			return
		}
		a.mu.Lock()
		a.state.AnimeOK = false
		a.state.AnimeMessage = err.Error()
		a.save()
		a.mu.Unlock()
		last.Status = "error"
		last.Errors = 1
		last.Message = err.Error()
		a.finish(last)
		return
	}
	a.mu.Lock()
	a.state.AnimeOK = true
	a.state.AnimeMessage = fmt.Sprintf("Sesión válida · %d entradas leídas", len(items))
	a.save()
	a.mu.Unlock()
	last.Found = len(items)
	a.mu.Lock()
	a.progressTotal = len(items)
	a.progressMessage = "Procesando biblioteca"
	a.mu.Unlock()

	revalidateAfter := time.Duration(getenvInt("CACHE_REVALIDATE_HOURS", 24)) * time.Hour
	now := time.Now()
	cancelled := false

	for idx, it := range items {
		select {
		case <-ctx.Done():
			cancelled = true
			break
		default:
		}
		if cancelled {
			break
		}

		a.mu.Lock()
		a.progressProcessed = idx
		a.progressMessage = "Procesando: " + it.Title
		a.mu.Unlock()

		status := malStatus(it.Status)
		cache, cached := a.cacheGet(it.MediaID)
		// Caché negativa: no repetir durante 24 h las mismas búsquedas costosas.
		if cached && cache.MALID == 0 && cache.NegativeUntil > now.Unix() && cache.SourceTitle == normalize(it.Title) {
			last.Errors++
			last.Unmatched = append(last.Unmatched, it.Title+": "+cache.NegativeReason+" (caché negativa)")
			last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, Status: status, Result: "error", Message: cache.NegativeReason + " (caché negativa)"})
			a.mu.Lock()
			a.progressProcessed = idx + 1
			a.progressMessage = "Omitido por caché negativa: " + it.Title
			a.mu.Unlock()
			continue
		}
		// Revalida también la identidad del título. Las versiones anteriores podían
		// guardar coincidencias que solo compartían el número de temporada.
		if cached && cache.MALID > 0 {
			_, safe := evaluateTitlePair(it.Title, cache.MALTitle, true)
			if !safe {
				a.cacheDelete(it.MediaID)
				cache = CacheEntry{}
				cached = false
			}
		}
		fresh := cached && cache.MatcherVersion == appVersion && cache.LastValidated > 0 && now.Sub(time.Unix(cache.LastValidated, 0)) < revalidateAfter
		unchanged := cached && sourceUnchanged(cache, it)
		desiredCached := desiredFor(it, cache.SourceTotal)
		cacheAlreadyCorrect := cache.MALSeen == desiredCached && cache.MALStatus == status
		cacheProtected := only && desiredCached < cache.MALSeen

		// Ruta rápida: la fuente no cambió, MAL fue validado recientemente y el estado cacheado ya es correcto.
		if unchanged && fresh && cache.MALID2 == 0 && (cacheAlreadyCorrect || cacheProtected) {
			last.Skipped++
			last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: cache.MALID, MALTitle: cache.MALTitle, MatchScore: cache.MatchScore, From: cache.MALSeen, To: desiredCached, Status: status, Result: "skipped", Message: "Sin cambios (caché)"})
			a.mu.Lock()
			a.progressProcessed = idx + 1
			a.progressMessage = "Sin cambios (caché): " + it.Title
			a.mu.Unlock()
			continue
		}

		var anime MALAnime
		matchScore := 0
		needResolve := !cached || cache.MALID == 0 || cache.SourceTitle != normalize(it.Title)
		needValidate := cached && (!fresh || !unchanged || cache.MALID2 > 0)

		if !needResolve && needValidate {
			anime, err = a.getCachedMAL(ctx, cache)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
					break
				}
				// El ID cacheado puede haber quedado obsoleto: se resuelve de nuevo por título.
				a.cacheDelete(it.MediaID)
				needResolve = true
			} else {
				matchScore = cache.MatchScore
			}
		}

		if needResolve {
			anime, matchScore, err = a.resolve(ctx, it)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
					break
				}
				last.Errors++
				negativeHours := getenvInt("NEGATIVE_CACHE_HOURS", 24)
				a.cachePut(CacheEntry{MediaID: it.MediaID, SourceTitle: normalize(it.Title), SourceSeen: it.Seen, SourceStatus: it.Status, SourceTotal: it.Total, NegativeUntil: time.Now().Add(time.Duration(negativeHours) * time.Hour).Unix(), NegativeReason: err.Error(), MatcherVersion: appVersion, UpdatedAt: time.Now().Unix()})
				last.Unmatched = append(last.Unmatched, it.Title+": "+err.Error())
				last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, Status: status, Result: "error", Message: err.Error()})
				continue
			}
		} else if !needValidate {
			anime = MALAnime{ID: cache.MALID, Title: cache.MALTitle, NumEpisodes: cache.SourceTotal}
			anime.MyListStatus = &struct {
				Status             string `json:"status"`
				NumEpisodesWatched int    `json:"num_episodes_watched"`
			}{Status: cache.MALStatus, NumEpisodesWatched: cache.MALSeen}
			matchScore = cache.MatchScore
		}

		// AnimeAV1 puede agrupar una temporada completa mientras MAL la divide en
		// una ficha principal y otra "Part 2". En ese caso se actualizan ambas en
		// orden: primero se llena la parte 1 y el excedente pasa a la parte 2.
		var anime2 MALAnime
		if cache.MALID2 > 0 && !needResolve {
			anime2, err = a.getCachedMAL2(ctx, cache)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
					break
				}
				anime2 = MALAnime{}
			} else if !validSplitPair(anime, anime2) {
				// Descarta emparejamientos peligrosos guardados por 1.5.1, como
				// Mushoku Tensei + 86 Part 2 o Kill la Kill + Luv(sic) Part 2.
				anime2 = MALAnime{}
				cache.MALID2 = 0
				cache.MALTitle2 = ""
				cache.MatchType = ""
			}
		}
		if anime2.ID == 0 && it.Total > anime.NumEpisodes && anime.NumEpisodes > 0 {
			anime2, _, err = a.resolveSplitPart2(ctx, it, anime)
			if err != nil && (errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)) {
				cancelled = true
				break
			}
		}
		if anime2.ID > 0 {
			desired1, desired2 := splitDesired(it, anime.NumEpisodes, anime2.NumEpisodes)
			status1 := splitStatus(status, desired1, anime.NumEpisodes, false)
			status2 := splitStatus(status, desired2, anime2.NumEpisodes, true)
			old1, _ := animeState(anime)
			old2, _ := animeState(anime2)
			new1, newStatus1, changed1, e1 := a.updateMALItem(ctx, anime, desired1, status1, dry, only)
			if e1 != nil {
				if errors.Is(e1, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
					break
				}
				last.Errors++
				last.Unmatched = append(last.Unmatched, it.Title+": "+e1.Error())
				last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MALID2: anime2.ID, MALTitle2: anime2.Title, MatchScore: matchScore, From: old1 + old2, To: desired1 + desired2, Status: status, Result: "error", Message: e1.Error()})
				continue
			}
			new2, newStatus2, changed2, e2 := a.updateMALItem(ctx, anime2, desired2, status2, dry, only)
			if e2 != nil {
				if errors.Is(e2, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
					break
				}
				last.Errors++
				last.Unmatched = append(last.Unmatched, it.Title+": "+e2.Error())
				last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MALID2: anime2.ID, MALTitle2: anime2.Title, MatchScore: matchScore, From: old1 + old2, To: desired1 + desired2, Status: status, Result: "error", Message: "Parte 2: " + e2.Error()})
				continue
			}
			entry := CacheEntry{MediaID: it.MediaID, MALID: anime.ID, MALTitle: anime.Title, MALID2: anime2.ID, MALTitle2: anime2.Title, MAL2Episodes: anime2.NumEpisodes, MAL2Seen: new2, MAL2Status: newStatus2, MatchType: "split_season", MatchScore: matchScore, SourceTitle: normalize(it.Title), SourceSeen: it.Seen, SourceStatus: it.Status, SourceTotal: it.Total, MALSeen: new1, MALStatus: newStatus1, LastValidated: time.Now().Unix(), UpdatedAt: time.Now().Unix(), MatcherVersion: appVersion, SearchStrategy: "title+split_part_2"}
			a.cachePut(entry)
			msg := fmt.Sprintf("Temporada partida: %s %d/%d + %s %d/%d", anime.Title, desired1, anime.NumEpisodes, anime2.Title, desired2, anime2.NumEpisodes)
			if changed1 || changed2 {
				last.Updated++
				if dry {
					msg = "Simulado · " + msg
				}
				last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MALID2: anime2.ID, MALTitle2: anime2.Title, MatchScore: matchScore, From: old1 + old2, To: desired1 + desired2, Status: status, Result: "updated", Message: msg})
				a.appendHistory(map[string]any{"ts": time.Now().Unix(), "title": it.Title, "animeav1_media_id": it.MediaID, "match_type": "split_season", "mal_id": anime.ID, "mal_title": anime.Title, "mal_id_2": anime2.ID, "mal_title_2": anime2.Title, "from_1": old1, "to_1": desired1, "from_2": old2, "to_2": desired2, "dry_run": dry})
			} else {
				last.Skipped++
				last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MALID2: anime2.ID, MALTitle2: anime2.Title, MatchScore: matchScore, From: old1 + old2, To: desired1 + desired2, Status: status, Result: "skipped", Message: msg + " · ya estaba sincronizado"})
			}
			a.mu.Lock()
			a.progressProcessed = idx + 1
			a.mu.Unlock()
			continue
		}

		current, currentStatus := animeState(anime)
		desired := desiredFor(it, anime.NumEpisodes)
		entry := CacheEntry{
			MediaID: it.MediaID, MALID: anime.ID, MALTitle: anime.Title, MatchScore: matchScore,
			SourceTitle: normalize(it.Title), SourceSeen: it.Seen, SourceStatus: it.Status, SourceTotal: it.Total,
			MALSeen: current, MALStatus: currentStatus, LastValidated: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
			MatcherVersion: appVersion, SearchStrategy: "multi_query",
		}

		if only && desired < current {
			last.Skipped++
			last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MatchScore: matchScore, From: current, To: desired, Status: status, Result: "skipped", Message: "Protegido por solo aumentar episodios"})
			a.cachePut(entry)
			a.mu.Lock()
			a.progressProcessed = idx + 1
			a.mu.Unlock()
			continue
		}
		if desired == current && currentStatus == status {
			last.Skipped++
			last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MatchScore: matchScore, From: current, To: desired, Status: status, Result: "skipped", Message: "Ya estaba sincronizado"})
			a.cachePut(entry)
			a.mu.Lock()
			a.progressProcessed = idx + 1
			a.mu.Unlock()
			continue
		}
		if !dry {
			vals := url.Values{"status": {status}, "num_watched_episodes": {strconv.Itoa(desired)}}
			if err := a.malRequestContext(ctx, "PUT", fmt.Sprintf("/anime/%d/my_list_status", anime.ID), vals, nil); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
					break
				}
				last.Errors++
				negativeHours := getenvInt("NEGATIVE_CACHE_HOURS", 24)
				a.cachePut(CacheEntry{MediaID: it.MediaID, SourceTitle: normalize(it.Title), SourceSeen: it.Seen, SourceStatus: it.Status, SourceTotal: it.Total, NegativeUntil: time.Now().Add(time.Duration(negativeHours) * time.Hour).Unix(), NegativeReason: err.Error(), MatcherVersion: appVersion, UpdatedAt: time.Now().Unix()})
				last.Unmatched = append(last.Unmatched, it.Title+": "+err.Error())
				last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MatchScore: matchScore, From: current, To: desired, Status: status, Result: "error", Message: err.Error()})
				continue
			}
			entry.MALSeen = desired
			entry.MALStatus = status
			entry.LastValidated = time.Now().Unix()
			entry.UpdatedAt = entry.LastValidated
		}
		a.cachePut(entry)
		last.Updated++
		last.Items = append(last.Items, RunItem{MediaID: it.MediaID, SourceTitle: it.Title, MALID: anime.ID, MALTitle: anime.Title, MatchScore: matchScore, From: current, To: desired, Status: status, Result: "updated", Message: map[bool]string{true: "Simulado", false: "Actualizado"}[dry]})
		a.appendHistory(map[string]any{"ts": time.Now().Unix(), "title": it.Title, "animeav1_media_id": it.MediaID, "mal_id": anime.ID, "mal_title": anime.Title, "match_score": matchScore, "from": current, "to": desired, "status": status, "dry_run": dry})
		a.mu.Lock()
		a.progressProcessed = idx + 1
		a.mu.Unlock()
	}

	if cancelled {
		a.mu.Lock()
		processed := a.progressProcessed
		a.mu.Unlock()
		last.Status = "cancelled"
		last.Message = fmt.Sprintf("Detenida por el usuario: procesados %d de %d, actualizados %d, omitidos %d, errores %d", processed, last.Found, last.Updated, last.Skipped, last.Errors)
	} else {
		if last.Errors > 0 {
			last.Status = "partial"
		}
		last.Message = fmt.Sprintf("Encontrados %d, actualizados %d, omitidos %d, errores %d", last.Found, last.Updated, last.Skipped, last.Errors)
	}
	a.finish(last)
}

func (a *App) finish(last LastRun) {
	last.Finished = time.Now().Unix()
	a.mu.Lock()
	a.running = false
	a.cancelSync = nil
	a.state.Last = last
	a.progressMessage = last.Message
	if last.Status != "cancelled" {
		a.progressProcessed = a.progressTotal
	}
	a.save()
	a.mu.Unlock()
	a.appendHistory(last)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ok := a.state.Last.Status != "error" && a.state.Last.Status != "partial"
	cacheEntries := a.cacheCount()
	out := map[string]any{"ok": ok, "running": a.running, "cache_entries": cacheEntries, "progress_processed": a.progressProcessed, "progress_total": a.progressTotal, "progress_message": a.progressMessage, "progress_trigger": a.progressTrigger, "last_status": a.state.Last.Status, "last_finished": a.state.Last.Finished, "animeav1_session": a.state.AnimeOK, "mal_authorized": a.state.Token.AccessToken != "", "mal_user": a.state.MALUsername, "dry_run": a.state.Settings.DryRun, "auto_sync": a.state.Settings.AutoSync, "last": a.state.Last}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
func historyLocation() *time.Location {
	name := getenv("LOG_TIMEZONE", "Europe/Madrid")
	loc, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("zona horaria %q no disponible; se usará UTC: %v", name, err)
		return time.UTC
	}
	return loc
}

func historyUnixTimestamp(line string) int64 {
	var meta struct {
		TS       int64 `json:"ts"`
		Started  int64 `json:"started"`
		Finished int64 `json:"finished"`
	}
	if json.Unmarshal([]byte(line), &meta) != nil {
		return 0
	}
	if meta.TS > 0 {
		return meta.TS
	}
	if meta.Finished > 0 {
		return meta.Finished
	}
	return meta.Started
}

func (a *App) recentHistoryText(limit int) string {
	b, err := os.ReadFile(filepath.Join(a.dataDir, "history.jsonl"))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "Sin historial"
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	loc := historyLocation()
	formatted := make([]string, 0, minInt(len(lines), limit))
	for i := len(lines) - 1; i >= 0 && len(formatted) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		stamp := "sin fecha"
		if unix := historyUnixTimestamp(line); unix > 0 {
			stamp = time.Unix(unix, 0).In(loc).Format("2006-01-02 15:04:05 MST")
		}
		formatted = append(formatted, "["+stamp+"] "+line)
	}
	return strings.Join(formatted, "\n")
}

func (a *App) logsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"text": a.recentHistoryText(40)})
}

func (a *App) cacheAPI(w http.ResponseWriter, r *http.Request) {
	a.cacheMu.Lock()
	items := make([]CacheEntry, 0, len(a.cache))
	for _, entry := range a.cache {
		items = append(items, entry)
	}
	a.cacheMu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].MediaID < items[j].MediaID })
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"items": items, "count": len(items)})
}

func (a *App) openAnimeAV1(w http.ResponseWriter, r *http.Request) {
	mediaID := IDString(strings.TrimSpace(r.URL.Query().Get("media_id")))
	if mediaID == "" {
		http.Error(w, "media_id no válido", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	cookie := a.state.Settings.Cookie
	a.mu.Unlock()
	if cookie == "" {
		http.Error(w, "Configura primero la cookie de AnimeAV1", http.StatusUnauthorized)
		return
	}
	items, err := a.scrapeContext(r.Context(), cookie)
	if err != nil {
		http.Error(w, "No se pudo consultar AnimeAV1: "+err.Error(), http.StatusBadGateway)
		return
	}
	for _, item := range items {
		if item.MediaID == mediaID {
			if strings.TrimSpace(item.Slug) == "" {
				http.Error(w, "AnimeAV1 no devolvió la URL de esta ficha", http.StatusNotFound)
				return
			}
			target := "https://animeav1.com/media/" + url.PathEscape(strings.TrimSpace(item.Slug))
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
	}
	http.Error(w, "El ID no se encontró en la biblioteca actual de AnimeAV1", http.StatusNotFound)
}

func (a *App) deleteCacheEntryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "se requiere POST"})
		return
	}
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "detén la sincronización antes de modificar la caché"})
		return
	}
	mediaID := IDString(strings.TrimSpace(r.FormValue("media_id")))
	if mediaID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "media_id no válido"})
		return
	}
	var err error
	a.cacheMu.Lock()
	entry, exists := a.cache[string(mediaID)]
	if exists {
		delete(a.cache, string(mediaID))
		err = a.saveCacheLocked()
	}
	a.cacheMu.Unlock()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "la coincidencia ya no existe"})
		return
	}
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "cache_entry_deleted", "media_id": mediaID, "source_title": entry.SourceTitle, "mal_id": entry.MALID, "mal_title": entry.MALTitle, "message": "Coincidencia eliminada manualmente de la caché"})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "media_id": mediaID, "count": a.cacheCount()})
}

func (a *App) resolveLastMatchingError(mediaID IDString, sourceTitle string) (int, string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	filtered := make([]RunItem, 0, len(a.state.Last.Items))
	for _, item := range a.state.Last.Items {
		if item.MediaID == mediaID && item.Result == "error" {
			continue
		}
		filtered = append(filtered, item)
	}
	a.state.Last.Items = filtered

	unmatched := make([]string, 0, len(a.state.Last.Unmatched))
	prefix := sourceTitle + ":"
	for _, item := range a.state.Last.Unmatched {
		if item == sourceTitle || strings.HasPrefix(item, prefix) {
			continue
		}
		unmatched = append(unmatched, item)
	}
	a.state.Last.Unmatched = unmatched

	errorsRemaining := 0
	for _, item := range a.state.Last.Items {
		if item.Result == "error" {
			errorsRemaining++
		}
	}
	a.state.Last.Errors = errorsRemaining
	if errorsRemaining == 0 && a.state.Last.Status == "partial" {
		a.state.Last.Status = "ok"
	}
	a.state.Last.Message = fmt.Sprintf("Encontrados %d, actualizados %d, omitidos %d, errores %d", a.state.Last.Found, a.state.Last.Updated, a.state.Last.Skipped, a.state.Last.Errors)
	a.progressMessage = a.state.Last.Message
	a.save()
	return a.state.Last.Errors, a.state.Last.Status, a.state.Last.Message
}

func (a *App) manualCacheEntryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "se requiere POST"})
		return
	}
	a.mu.Lock()
	running := a.running
	cookie := a.state.Settings.Cookie
	a.mu.Unlock()
	if running {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "detén la sincronización antes de modificar la caché"})
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	mediaID := IDString(strings.TrimSpace(r.FormValue("media_id")))
	if mediaID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "media_id no válido"})
		return
	}
	malID, err := strconv.Atoi(r.FormValue("mal_id"))
	if err != nil || malID <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "mal_id no válido"})
		return
	}
	malID2 := 0
	if raw := strings.TrimSpace(r.FormValue("mal_id_2")); raw != "" {
		malID2, err = strconv.Atoi(raw)
		if err != nil || malID2 <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "mal_id_2 no válido"})
			return
		}
	}
	items, err := a.scrapeContext(r.Context(), cookie)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "no se pudo leer AnimeAV1: " + err.Error()})
		return
	}
	var source AVItem
	found := false
	for _, item := range items {
		if item.MediaID == mediaID {
			source = item
			found = true
			break
		}
	}
	if !found {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID de AnimeAV1 no encontrado en tu biblioteca"})
		return
	}
	fields := "id,title,alternative_titles,num_episodes,media_type,start_date,my_list_status"
	var anime MALAnime
	if err := a.malRequestContext(r.Context(), http.MethodGet, fmt.Sprintf("/anime/%d?fields=%s", malID, fields), nil, &anime); err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID de MAL no válido: " + err.Error()})
		return
	}
	seen, status := animeState(anime)
	entry := CacheEntry{MediaID: mediaID, MALID: anime.ID, MALTitle: anime.Title, MatchType: "manual", MatchScore: 999, SourceTitle: normalize(source.Title), SourceSeen: source.Seen, SourceStatus: source.Status, SourceTotal: source.Total, MALSeen: seen, MALStatus: status, LastValidated: time.Now().Unix(), UpdatedAt: time.Now().Unix(), MatcherVersion: appVersion, SearchStrategy: "manual_mal_id"}
	if malID2 > 0 {
		var anime2 MALAnime
		if err := a.malRequestContext(r.Context(), http.MethodGet, fmt.Sprintf("/anime/%d?fields=%s", malID2, fields), nil, &anime2); err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "segundo ID de MAL no válido: " + err.Error()})
			return
		}
		seen2, status2 := animeState(anime2)
		entry.MALID2 = anime2.ID
		entry.MALTitle2 = anime2.Title
		entry.MAL2Episodes = anime2.NumEpisodes
		entry.MAL2Seen = seen2
		entry.MAL2Status = status2
		entry.MatchType = "manual_split"
	}
	a.cachePut(entry)
	errorsRemaining, lastStatus, lastMessage := a.resolveLastMatchingError(mediaID, source.Title)
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "manual_match_saved", "media_id": mediaID, "source_title": source.Title, "mal_id": entry.MALID, "mal_title": entry.MALTitle, "mal_id_2": entry.MALID2, "mal_title_2": entry.MALTitle2})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "entry": entry, "errors": errorsRemaining, "status": lastStatus, "message": lastMessage})
}

type candidateDebug struct {
	MALID     int    `json:"mal_id"`
	MALTitle  string `json:"mal_title"`
	Score     int    `json:"score"`
	Accepted  bool   `json:"accepted"`
	Reason    string `json:"reason"`
	Episodes  int    `json:"episodes"`
	MediaType string `json:"media_type"`
}

func (a *App) inspectCandidates(ctx context.Context, it AVItem) ([]candidateDebug, error) {
	ids := map[int]bool{}
	var lastErr error
	for _, title := range a.candidateTitles(it) {
		for _, query := range searchQueries(title) {
			var sr MALSearch
			if err := a.malRequestContext(ctx, "GET", "/anime?q="+url.QueryEscape(query)+"&limit=100", nil, &sr); err != nil {
				lastErr = err
				continue
			}
			for _, x := range sr.Data {
				ids[x.Node.ID] = true
			}
		}
	}
	out := make([]candidateDebug, 0, len(ids))
	for id := range ids {
		var anime MALAnime
		if err := a.malRequestContext(ctx, "GET", fmt.Sprintf("/anime/%d?fields=id,title,alternative_titles,num_episodes,media_type,start_date", id), nil, &anime); err != nil {
			continue
		}
		best := -1
		reason := "título/base insuficiente"
		for _, sourceTitle := range a.candidateTitles(it) {
			primary := normalize(sourceTitle) == normalize(it.Title)
			for _, malTitle := range animeTitles(anime) {
				m, ok := evaluateTitlePair(sourceTitle, malTitle, primary)
				if ok && m.score > best {
					best = m.score
					reason = "coincidencia aceptable"
				}
			}
		}
		if hasSeasonMarker(it.Title) && partNumber(anime.Title) > 0 && !hasSeasonMarker(anime.Title) {
			reason = "rechazado: Part 2 no equivale a Season 2"
			best = -1
		}
		out = append(out, candidateDebug{MALID: anime.ID, MALTitle: anime.Title, Score: best, Accepted: best >= getenvInt("TITLE_MATCH_THRESHOLD", 88), Reason: reason, Episodes: anime.NumEpisodes, MediaType: anime.MediaType})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Accepted != out[j].Accepted {
			return out[i].Accepted
		}
		return out[i].Score > out[j].Score
	})
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

func (a *App) cacheCandidatesAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	mediaID := IDString(strings.TrimSpace(r.URL.Query().Get("media_id")))
	entry, ok := a.cacheGet(mediaID)
	if !ok {
		http.Error(w, "entrada no encontrada", http.StatusNotFound)
		return
	}
	it := AVItem{MediaID: entry.MediaID, Title: entry.SourceTitle, Seen: entry.SourceSeen, Total: entry.SourceTotal, Status: entry.SourceStatus}
	items, err := a.inspectCandidates(r.Context(), it)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"source_title": entry.SourceTitle, "items": items})
}

func (a *App) recomputeCacheEntryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		http.Error(w, "detén la sincronización antes de recalcular", http.StatusConflict)
		return
	}
	mediaID := IDString(strings.TrimSpace(r.FormValue("media_id")))
	old, ok := a.cacheGet(mediaID)
	if !ok {
		http.Error(w, "entrada no encontrada", http.StatusNotFound)
		return
	}
	it := AVItem{MediaID: old.MediaID, Title: old.SourceTitle, Seen: old.SourceSeen, Total: old.SourceTotal, Status: old.SourceStatus}
	anime, score, err := a.resolve(r.Context(), it)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	seen, status := animeState(anime)
	entry := old
	entry.MALID = anime.ID
	entry.MALTitle = anime.Title
	entry.MatchScore = score
	entry.MALSeen = seen
	entry.MALStatus = status
	entry.LastValidated = time.Now().Unix()
	entry.UpdatedAt = time.Now().Unix()
	entry.MatcherVersion = appVersion
	entry.SearchStrategy = "multi_query"
	a.cachePut(entry)
	a.appendHistory(map[string]any{"ts": time.Now().Unix(), "event": "cache_entry_recomputed", "media_id": mediaID, "source_title": entry.SourceTitle, "mal_id": entry.MALID, "mal_title": entry.MALTitle, "message": "Coincidencia recalculada manualmente"})
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "entry": entry})
}

func (a *App) clearHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		http.Error(w, "No se puede borrar el historial durante una sincronización", http.StatusConflict)
		return
	}
	path := filepath.Join(a.dataDir, "history.jsonl")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectHome(w, r)
}

func (a *App) history(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, a.recentHistoryText(200))
}

func (a *App) historyRaw(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join(a.dataDir, "history.jsonl"))
	if err != nil {
		b = []byte("Sin historial")
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Write(b)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
