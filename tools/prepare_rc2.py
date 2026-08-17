from pathlib import Path

# Prevent two different MAL entries from writing the same AnimeAV1 media item
# during one reverse-sync run. This is intentionally conservative: ambiguous
# multi-MAL -> one-AV1 relationships must be resolved instead of letting the
# later MAL item overwrite the earlier status/progress.
p = Path('reverse_runtime.go')
s = p.read_text(encoding='utf-8')

old = '\tconflicts := make([]ReverseConflict, 0)\n\tfor idx, mal := range malItems {'
new = '\tconflicts := make([]ReverseConflict, 0)\n\tclaimedMedia := map[string]MALListItem{}\n\tfor idx, mal := range malItems {'
if old in s:
    s = s.replace(old, new, 1)
elif 'claimedMedia := map[string]MALListItem{}' not in s:
    raise SystemExit('claimedMedia anchor not found')

anchor = '''\t\tmediaID, _, score, err := a.resolveAnimeAV1Media(ctx, cookie, mal)\n\t\tif err != nil {\n\t\t\tlast.Errors++\n\t\t\tlast.Unmatched = append(last.Unmatched, mal.Title+": "+err.Error())\n\t\t\tlast.Items = append(last.Items, RunItem{MALID: mal.ID, MALTitle: mal.Title, SourceTitle: mal.Title, MatchScore: score, Result: "error", Message: err.Error()})\n\t\t\tcontinue\n\t\t}\n\n'''
insert = anchor + '''\t\tclaimKey := string(mediaID)\n\t\tif previous, ok := claimedMedia[claimKey]; ok && previous.ID != mal.ID {\n\t\t\tmsg := fmt.Sprintf("Colisión de coincidencia: MAL #%d (%s) y MAL #%d (%s) apuntan al mismo AnimeAV1 media_id=%s. No se modifica la segunda entrada; requiere revisión manual.", previous.ID, previous.Title, mal.ID, mal.Title, mediaID)\n\t\t\tlast.Errors++\n\t\t\tlast.Items = append(last.Items, RunItem{MediaID: mediaID, MALID: mal.ID, MALTitle: mal.Title, SourceTitle: mal.Title, MatchScore: score, Result: "error", Message: msg})\n\t\t\tcontinue\n\t\t}\n\t\tclaimedMedia[claimKey] = mal\n\n'''
if 'Colisión de coincidencia:' not in s:
    if anchor not in s:
        raise SystemExit('resolve anchor not found')
    s = s.replace(anchor, insert, 1)
p.write_text(s, encoding='utf-8')

main = Path('main.go')
m = main.read_text(encoding='utf-8').replace('appVersion = "1.7.0-rc1"', 'appVersion = "1.7.0-rc2"')
main.write_text(m, encoding='utf-8')

Path('VERSION').write_text('1.7.0-rc2\n', encoding='utf-8')
compose = Path('docker-compose.portainer.yml')
c = compose.read_text(encoding='utf-8').replace('ovelayos/animeav1-mal-sync:1.7.0-rc1', 'ovelayos/animeav1-mal-sync:1.7.0-rc2')
compose.write_text(c, encoding='utf-8')
