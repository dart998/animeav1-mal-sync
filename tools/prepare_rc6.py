from pathlib import Path

main = Path('main.go')
s = main.read_text(encoding='utf-8')

if 'appVersion = "1.7.0-rc5"' not in s:
    raise SystemExit('RC5 version anchor not found')
s = s.replace('appVersion = "1.7.0-rc5"', 'appVersion = "1.7.0-rc6"', 1)

old = '''func main() {
\tapp := &App{'''
new = '''func main() {
\tif len(os.Args) > 1 && os.Args[1] == "healthcheck" {
\t\tclient := &http.Client{Timeout: 4 * time.Second}
\t\tresp, err := client.Get("http://127.0.0.1:8787/health")
\t\tif err != nil {
\t\t\tos.Exit(1)
\t\t}
\t\tdefer resp.Body.Close()
\t\t_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
\t\tif resp.StatusCode < 200 || resp.StatusCode >= 300 {
\t\t\tos.Exit(1)
\t\t}
\t\treturn
\t}

\tapp := &App{'''
if old not in s:
    raise SystemExit('main anchor not found')
s = s.replace(old, new, 1)

old = '''\tmux.HandleFunc("/health", app.health)
\tmux.HandleFunc("/api/status", app.health)
\tmux.HandleFunc("/api/logs", app.logsAPI)'''
new = '''\tmux.HandleFunc("/health", app.healthCheckHTTP)
\tmux.HandleFunc("/api/status", app.health)
\tmux.HandleFunc("/api/logs", app.logsAPI)
\tmux.HandleFunc("/log", app.history)'''
if old not in s:
    raise SystemExit('route anchor not found')
s = s.replace(old, new, 1)

old = '''func favicon(w http.ResponseWriter, r *http.Request) {'''
new = '''func (a *App) healthCheckHTTP(w http.ResponseWriter, r *http.Request) {
\tw.Header().Set("Content-Type", "application/json; charset=utf-8")
\tw.Header().Set("Cache-Control", "no-store")
\tw.WriteHeader(http.StatusOK)
\t_, _ = fmt.Fprintf(w, `{"status":"ok","version":%q}`, appVersion)
}

func favicon(w http.ResponseWriter, r *http.Request) {'''
if old not in s:
    raise SystemExit('favicon anchor not found')
s = s.replace(old, new, 1)

old = '''href="/health">JSON</a> <a class="btn secondary" target="_blank" rel="noopener" href="/history">Logs</a>'''
new = '''href="/api/status">JSON</a> <a class="btn secondary" target="_blank" rel="noopener" href="/log">Logs</a>'''
if old not in s:
    raise SystemExit('dashboard links anchor not found')
s = s.replace(old, new, 1)
main.write_text(s, encoding='utf-8')

Path('VERSION').write_text('1.7.0-rc6\n', encoding='utf-8')

compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8')
if 'ovelayos/animeav1-mal-sync:1.7.0-rc5' not in c:
    raise SystemExit('compose RC5 image anchor not found')
c = c.replace('ovelayos/animeav1-mal-sync:1.7.0-rc5', 'ovelayos/animeav1-mal-sync:1.7.0-rc6', 1)
compose.write_text(c, encoding='utf-8')

dockerfile = Path('Dockerfile')
d = dockerfile.read_text(encoding='utf-8')
health = 'HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["/animeav1-mal-sync", "healthcheck"]\n'
if 'HEALTHCHECK ' not in d:
    anchor = 'VOLUME ["/data"]\nENTRYPOINT ["/animeav1-mal-sync"]'
    if anchor not in d:
        raise SystemExit('Dockerfile anchor not found')
    d = d.replace(anchor, 'VOLUME ["/data"]\n' + health + 'ENTRYPOINT ["/animeav1-mal-sync"]', 1)
dockerfile.write_text(d, encoding='utf-8')
