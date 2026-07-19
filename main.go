package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
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
)

const (
	authURL  = "https://myanimelist.net/v1/oauth2/authorize"
	tokenURL = "https://myanimelist.net/v1/oauth2/token"
	apiBase  = "https://api.myanimelist.net/v2"
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
}

type LastRun struct {
	Status    string   `json:"status"`
	Started   int64    `json:"started"`
	Finished  int64    `json:"finished"`
	Found     int      `json:"found"`
	Updated   int      `json:"updated"`
	Skipped   int      `json:"skipped"`
	Errors    int      `json:"errors"`
	Message   string   `json:"message"`
	Unmatched []string `json:"unmatched"`
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
	mu      sync.Mutex
	state   State
	running bool
	dataDir string
	client  *http.Client
}

type AVItem struct {
	Title  string
	Seen   int
	Total  int
	Status string
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
	ID           int    `json:"id"`
	Title        string `json:"title"`
	NumEpisodes  int    `json:"num_episodes"`
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
	app := &App{
		dataDir: getenv("DATA_DIR", "/data"),
		client:  &http.Client{Timeout: 45 * time.Second},
	}
	if err := os.MkdirAll(app.dataDir, 0755); err != nil {
		log.Fatal(err)
	}
	app.load()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.dashboard)
	mux.HandleFunc("/health", app.health)
	mux.HandleFunc("/api/status", app.health)
	mux.HandleFunc("/cookie", app.saveCookie)
	mux.HandleFunc("/settings", app.saveSettings)
	mux.HandleFunc("/check", app.check)
	mux.HandleFunc("/sync", app.syncHandler)
	mux.HandleFunc("/oauth/start", app.oauthStart)
	mux.HandleFunc("/oauth/callback", app.oauthCallback)
	mux.HandleFunc("/oauth/disconnect", app.oauthDisconnect)
	mux.HandleFunc("/history", app.history)

	go app.scheduler()
	addr := getenv("LISTEN_ADDR", ":8787")
	log.Printf("AnimeAV1 MAL Sync escuchando en %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (a *App) load() {
	a.state.Settings.IntervalMinutes = getenvInt("SYNC_INTERVAL_MINUTES", 15)
	a.state.Settings.DryRun = getenvBool("DRY_RUN", true)
	a.state.Settings.OnlyIncrease = getenvBool("ONLY_INCREASE", true)
	b, err := os.ReadFile(filepath.Join(a.dataDir, "state.json"))
	if err == nil {
		_ = json.Unmarshal(b, &a.state)
	}
	if a.state.Settings.IntervalMinutes <= 0 {
		a.state.Settings.IntervalMinutes = 15
	}
}
func (a *App) save() {
	b, _ := json.MarshalIndent(a.state, "", "  ")
	_ = os.WriteFile(filepath.Join(a.dataDir, "state.json"), b, 0600)
}
func (a *App) appendHistory(v any) {
	b, _ := json.Marshal(v)
	f, err := os.OpenFile(filepath.Join(a.dataDir, "history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		defer f.Close()
		f.Write(append(b, '\n'))
	}
}

func (a *App) scheduler() {
	for {
		a.mu.Lock()
		mins := a.state.Settings.IntervalMinutes
		last := a.state.Last.Finished
		running := a.running
		a.mu.Unlock()
		if mins < 1 {
			mins = 15
		}
		if !running && (last == 0 || time.Now().Unix()-last >= int64(mins*60)) {
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
	s := a.state
	running := a.running
	a.mu.Unlock()
	cookieStatus := "❌ Sin configurar"
	if s.Settings.Cookie != "" {
		cookieStatus = "⚠️ Guardada, sin verificar"
	}
	if s.AnimeOK {
		cookieStatus = "✅ " + s.AnimeMessage
	}
	malStatus := "❌ No autorizado"
	if s.Token.AccessToken != "" {
		malStatus = "✅ Autorizado"
		if s.MALUsername != "" {
			malStatus += " como " + html.EscapeString(s.MALUsername)
		}
	}
	last := s.Last.Status
	if last == "" {
		last = "Nunca"
	}
	runText := "No"
	if running {
		runText = "Sí"
	}
	page := fmt.Sprintf(`<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>AnimeAV1 → MAL</title><style>
body{font-family:Arial,sans-serif;background:#111827;color:#e5e7eb;max-width:900px;margin:30px auto;padding:0 16px}h1{margin-bottom:8px}.card{background:#1f2937;border-radius:12px;padding:20px;margin:16px 0}input,textarea{width:100%%;box-sizing:border-box;background:#111827;color:#fff;border:1px solid #4b5563;border-radius:8px;padding:10px;margin:6px 0 12px}button,.btn{display:inline-block;background:#14b8a6;color:#041311;border:0;border-radius:8px;padding:10px 15px;font-weight:bold;text-decoration:none;cursor:pointer}.secondary{background:#374151;color:#fff}.danger{background:#ef4444;color:#fff}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:10px}.stat{background:#111827;padding:12px;border-radius:8px}.muted{color:#9ca3af}.msg{white-space:pre-wrap;word-break:break-word}</style></head><body>
<h1>AnimeAV1 → MyAnimeList</h1><div class="muted">Versión EX4100 ARMv7, sin navegador ni Python</div>
<div class="card"><h2>AnimeAV1</h2><p>%s</p><form method="post" action="/cookie"><label>Cookie completa del navegador</label><textarea name="cookie" rows="3" placeholder="session=...; otra_cookie=...">%s</textarea><button>Guardar cookie</button> <a class="btn secondary" href="/check">Verificar</a></form><p class="muted">Pega el contenido completo de la cabecera Cookie. Se guarda únicamente en /data/state.json.</p></div>
<div class="card"><h2>MyAnimeList</h2><p>%s</p><a class="btn" href="/oauth/start">Conectar con MAL</a> <a class="btn danger" href="/oauth/disconnect">Desconectar</a></div>
<div class="card"><h2>Sincronización</h2><form method="post" action="/settings"><label>Intervalo en minutos</label><input type="number" min="1" name="interval" value="%d"><label><input style="width:auto" type="checkbox" name="dry" %s> Modo simulación (no escribe en MAL)</label><br><label><input style="width:auto" type="checkbox" name="increase" %s> Solo aumentar episodios</label><br><br><button>Guardar ajustes</button> <button formaction="/sync">Sincronizar ahora</button></form></div>
<div class="card"><h2>Estado</h2><div class="grid"><div class="stat"><b>Ejecutándose</b><br>%s</div><div class="stat"><b>Último estado</b><br>%s</div><div class="stat"><b>Encontrados</b><br>%d</div><div class="stat"><b>Actualizados</b><br>%d</div><div class="stat"><b>Errores</b><br>%d</div></div><p class="msg">%s</p><p><a class="btn secondary" href="/health">JSON</a> <a class="btn secondary" href="/history">Historial</a></p></div>
</body></html>`, cookieStatus, html.EscapeString(s.Settings.Cookie), malStatus, s.Settings.IntervalMinutes, checked(s.Settings.DryRun), checked(s.Settings.OnlyIncrease), runText, html.EscapeString(last), s.Last.Found, s.Last.Updated, s.Last.Errors, html.EscapeString(s.Last.Message))
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
		n = 15
	}
	a.mu.Lock()
	a.state.Settings.IntervalMinutes = n
	a.state.Settings.DryRun = r.FormValue("dry") != ""
	a.state.Settings.OnlyIncrease = r.FormValue("increase") != ""
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
	if cookie == "" {
		return "", errors.New("falta la cookie")
	}
	urls := listURLs()
	req, _ := http.NewRequest("GET", urls[0].URL, nil)
	browserHeaders(req, cookie)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	body := strings.ToLower(string(b))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("AnimeAV1 respondió HTTP %d", resp.StatusCode)
	}
	if strings.Contains(body, "verifique que es un ser humano") || strings.Contains(body, "type=\"password\"") || strings.Contains(body, "iniciar sesión") {
		return "", errors.New("cookie caducada o sesión no válida")
	}
	return "Sesión válida", nil
}

type listDef struct{ Status, URL string }

func listURLs() []listDef {
	return []listDef{
		{"watching", getenv("ANIMEAV1_WATCHING_URL", "https://animeav1.com/cuenta/listas/viendo")},
		{"plan_to_watch", getenv("ANIMEAV1_PLAN_TO_WATCH_URL", "https://animeav1.com/cuenta/listas/por-ver")},
		{"completed", getenv("ANIMEAV1_COMPLETED_URL", "https://animeav1.com/cuenta/listas/completada")},
		{"dropped", getenv("ANIMEAV1_DROPPED_URL", "https://animeav1.com/cuenta/listas/dropped")},
	}
}
func browserHeaders(req *http.Request, cookie string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/537.36 Chrome/124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	req.Header.Set("Cookie", cookie)
}

var reProgress = regexp.MustCompile(`(?is)(.{0,1800}?)(\d+)\s*/\s*(\d+)(.{0,700}?)`)
var reHeading = regexp.MustCompile(`(?is)<h[1-6][^>]*>(.*?)</h[1-6]>`)
var reMediaLink = regexp.MustCompile(`(?is)<a[^>]+href=["'][^"']*/(?:media|anime|serie|ver)/[^"']+["'][^>]*>(.*?)</a>`)
var reTags = regexp.MustCompile(`<[^>]+>`)
var reSpaces = regexp.MustCompile(`\s+`)

func cleanText(v string) string {
	v = reTags.ReplaceAllString(v, " ")
	v = html.UnescapeString(v)
	return strings.TrimSpace(reSpaces.ReplaceAllString(v, " "))
}
func parseItems(body, status string) []AVItem {
	out := []AVItem{}
	seen := map[string]bool{}
	for _, m := range reProgress.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[2])
		total, _ := strconv.Atoi(m[3])
		block := m[1] + m[4]
		title := ""
		hs := reHeading.FindAllStringSubmatch(block, -1)
		if len(hs) > 0 {
			title = cleanText(hs[len(hs)-1][1])
		}
		if title == "" {
			ls := reMediaLink.FindAllStringSubmatch(block, -1)
			if len(ls) > 0 {
				title = cleanText(ls[len(ls)-1][1])
			}
		}
		if title == "" || len(title) > 180 {
			continue
		}
		key := normalize(title) + fmt.Sprint(n)
		if !seen[key] {
			seen[key] = true
			out = append(out, AVItem{title, n, total, status})
		}
	}
	return out
}
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

func (a *App) scrape(cookie string) ([]AVItem, error) {
	all := []AVItem{}
	for _, d := range listURLs() {
		req, _ := http.NewRequest("GET", d.URL, nil)
		browserHeaders(req, cookie)
		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
		resp.Body.Close()
		low := strings.ToLower(string(b))
		if strings.Contains(low, "iniciar sesión") || strings.Contains(low, "verifique que es un ser humano") {
			return nil, errors.New("la cookie de AnimeAV1 ha caducado")
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("%s devolvió HTTP %d", d.URL, resp.StatusCode)
		}
		all = append(all, parseItems(string(b), d.Status)...)
	}
	if len(all) == 0 {
		return nil, errors.New("no se encontraron entradas; comprueba las URL de listas o la cookie")
	}
	return all, nil
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
	if err := a.refreshIfNeeded(); err != nil {
		return err
	}
	a.mu.Lock()
	tok := a.state.Token.AccessToken
	a.mu.Unlock()
	var body io.Reader
	if vals != nil {
		body = strings.NewReader(vals.Encode())
	}
	req, _ := http.NewRequest(method, apiBase+path, body)
	req.Header.Set("Authorization", "Bearer "+tok)
	if vals != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("MAL HTTP %d: %s", resp.StatusCode, string(b))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
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
func (a *App) resolve(title string) (MALAnime, error) {
	q := url.QueryEscape(title)
	var s MALSearch
	if err := a.malRequest("GET", "/anime?q="+q+"&limit=10", nil, &s); err != nil {
		return MALAnime{}, err
	}
	best := 0
	bid := 0
	for _, x := range s.Data {
		sc := similarity(title, x.Node.Title)
		if sc > best {
			best = sc
			bid = x.Node.ID
		}
	}
	threshold := getenvInt("TITLE_MATCH_THRESHOLD", 80)
	if bid == 0 || best < threshold {
		return MALAnime{}, fmt.Errorf("sin coincidencia suficiente (%d%%)", best)
	}
	var anime MALAnime
	err := a.malRequest("GET", fmt.Sprintf("/anime/%d?fields=id,title,num_episodes,my_list_status", bid), nil, &anime)
	return anime, err
}
func malStatus(s string) string {
	switch s {
	case "watching":
		return "watching"
	case "completed":
		return "completed"
	case "dropped":
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
func (a *App) saveSettingsNoRedirect(r *http.Request) {
	n, _ := strconv.Atoi(r.FormValue("interval"))
	if n < 1 {
		n = 15
	}
	a.mu.Lock()
	a.state.Settings.IntervalMinutes = n
	a.state.Settings.DryRun = r.FormValue("dry") != ""
	a.state.Settings.OnlyIncrease = r.FormValue("increase") != ""
	a.save()
	a.mu.Unlock()
}
func (a *App) runSync(trigger string) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.running = true
	cookie := a.state.Settings.Cookie
	dry := a.state.Settings.DryRun
	only := a.state.Settings.OnlyIncrease
	a.state.Last = LastRun{Status: "running", Started: time.Now().Unix(), Message: "Sincronización " + trigger}
	a.save()
	a.mu.Unlock()
	last := LastRun{Status: "ok", Started: time.Now().Unix()}
	items, err := a.scrape(cookie)
	if err != nil {
		last.Status = "error"
		last.Errors = 1
		last.Message = err.Error()
		a.finish(last)
		return
	}
	last.Found = len(items)
	for _, it := range items {
		anime, err := a.resolve(it.Title)
		if err != nil {
			last.Errors++
			last.Unmatched = append(last.Unmatched, it.Title+": "+err.Error())
			continue
		}
		current := 0
		if anime.MyListStatus != nil {
			current = anime.MyListStatus.NumEpisodesWatched
		}
		desired := it.Seen
		if anime.NumEpisodes > 0 && desired > anime.NumEpisodes {
			desired = anime.NumEpisodes
		}
		if only && desired < current {
			last.Skipped++
			continue
		}
		status := malStatus(it.Status)
		if desired == current && anime.MyListStatus != nil && anime.MyListStatus.Status == status {
			last.Skipped++
			continue
		}
		if !dry {
			vals := url.Values{"status": {status}, "num_watched_episodes": {strconv.Itoa(desired)}}
			if err := a.malRequest("PUT", fmt.Sprintf("/anime/%d/my_list_status", anime.ID), vals, nil); err != nil {
				last.Errors++
				last.Unmatched = append(last.Unmatched, it.Title+": "+err.Error())
				continue
			}
		}
		last.Updated++
		a.appendHistory(map[string]any{"ts": time.Now().Unix(), "title": it.Title, "mal_title": anime.Title, "from": current, "to": desired, "status": status, "dry_run": dry})
	}
	if last.Errors > 0 {
		last.Status = "partial"
	}
	last.Message = fmt.Sprintf("Encontrados %d, actualizados %d, omitidos %d, errores %d", last.Found, last.Updated, last.Skipped, last.Errors)
	a.finish(last)
}
func (a *App) finish(last LastRun) {
	last.Finished = time.Now().Unix()
	a.mu.Lock()
	a.running = false
	a.state.Last = last
	a.save()
	a.mu.Unlock()
	a.appendHistory(last)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ok := a.state.Last.Status != "error" && a.state.Last.Status != "partial"
	out := map[string]any{"ok": ok, "running": a.running, "last_status": a.state.Last.Status, "last_finished": a.state.Last.Finished, "animeav1_session": a.state.AnimeOK, "mal_authorized": a.state.Token.AccessToken != "", "mal_user": a.state.MALUsername, "dry_run": a.state.Settings.DryRun, "last": a.state.Last}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join(a.dataDir, "history.jsonl"))
	if err != nil {
		b = []byte("Sin historial")
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	sort.SliceStable(lines, func(i, j int) bool { return i > j })
	if len(lines) > 200 {
		lines = lines[:200]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, strings.Join(lines, "\n"))
}
