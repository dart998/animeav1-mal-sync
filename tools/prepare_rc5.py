from pathlib import Path

# RC5: skip MAL entries that have not started airing yet and make dry-run text neutral.
reverse = Path('reverse_sync.go')
r = reverse.read_text(encoding='utf-8')

old = '''type MALListItem struct {
\tID        int
\tTitle     string
\tAliases   []string
\tEpisodes  int
\tSeen      int
\tStatus    string
\tMediaType string
}'''
new = '''type MALListItem struct {
\tID        int
\tTitle     string
\tAliases   []string
\tEpisodes  int
\tSeen      int
\tStatus    string
\tMediaType string
\tAirStatus string
\tStartDate string
}'''
if old not in r:
    raise SystemExit('MALListItem anchor not found')
r = r.replace(old, new, 1)

old = '''\t\t\tNumEpisodes       int    `json:"num_episodes"`
\t\t\tMediaType         string `json:"media_type"`
\t\t\tAlternativeTitles struct {'''
new = '''\t\t\tNumEpisodes       int    `json:"num_episodes"`
\t\t\tMediaType         string `json:"media_type"`
\t\t\tStatus            string `json:"status"`
\t\t\tStartDate         string `json:"start_date"`
\t\t\tAlternativeTitles struct {'''
if old not in r:
    raise SystemExit('malListPage node anchor not found')
r = r.replace(old, new, 1)

old = 'const fields = "list_status,num_episodes,media_type,alternative_titles"'
new = 'const fields = "list_status,num_episodes,media_type,alternative_titles,status,start_date"'
if old not in r:
    raise SystemExit('MAL fields anchor not found')
r = r.replace(old, new, 1)

old = '''\t\t\t\tSeen:      row.ListStatus.NumEpisodesWatched,
\t\t\t\tStatus:    row.ListStatus.Status,
\t\t\t\tMediaType: row.Node.MediaType,
\t\t\t})'''
new = '''\t\t\t\tSeen:      row.ListStatus.NumEpisodesWatched,
\t\t\t\tStatus:    row.ListStatus.Status,
\t\t\t\tMediaType: row.Node.MediaType,
\t\t\t\tAirStatus: row.Node.Status,
\t\t\t\tStartDate: row.Node.StartDate,
\t\t\t})'''
if old not in r:
    raise SystemExit('MALListItem population anchor not found')
r = r.replace(old, new, 1)
reverse.write_text(r, encoding='utf-8')

runtime = Path('reverse_runtime.go')
s = runtime.read_text(encoding='utf-8')
anchor = '''\t\tstatus, err := avStatusFromMAL(mal.Status)
\t\tif err != nil {'''
insert = '''\t\tif mal.AirStatus == "not_yet_aired" {
\t\t\tlast.Skipped++
\t\t\tmsg := "Próximamente · MAL indica que todavía no se ha estrenado; no se busca en AnimeAV1"
\t\t\tif mal.StartDate != "" {
\t\t\t\tmsg += " · estreno: " + mal.StartDate
\t\t\t}
\t\t\tlast.Items = append(last.Items, RunItem{MALID: mal.ID, MALTitle: mal.Title, SourceTitle: mal.Title, From: mal.Seen, To: mal.Seen, Status: mal.Status, Result: "skipped", Message: msg})
\t\t\ta.mu.Lock()
\t\t\ta.progressProcessed = idx + 1
\t\t\ta.progressMessage = "Próximamente en MAL: " + mal.Title
\t\t\ta.mu.Unlock()
\t\t\tcontinue
\t\t}

\t\tstatus, err := avStatusFromMAL(mal.Status)
\t\tif err != nil {'''
if anchor not in s:
    raise SystemExit('reverse runtime status anchor not found')
s = s.replace(anchor, insert, 1)
runtime.write_text(s, encoding='utf-8')

main = Path('main.go')
m = main.read_text(encoding='utf-8')
if 'appVersion = "1.7.0-rc4"' not in m:
    raise SystemExit('appVersion RC4 anchor not found')
m = m.replace('appVersion = "1.7.0-rc4"', 'appVersion = "1.7.0-rc5"', 1)
m = m.replace('Modo simulación (no escribe en MAL)', 'Modo simulación (no escribe cambios)', 1)
main.write_text(m, encoding='utf-8')

Path('VERSION').write_text('1.7.0-rc5\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8')
c = c.replace('ovelayos/animeav1-mal-sync:1.7.0-rc4', 'ovelayos/animeav1-mal-sync:1.7.0-rc5')
compose.write_text(c, encoding='utf-8')
