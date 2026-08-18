package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MALListItem representa una entrada de la lista del usuario en MAL para la
// sincronización inversa. AnimeAV1 sigue siendo el único destino en este modo.
type MALListItem struct {
	ID        int
	Title     string
	Aliases   []string
	Episodes  int
	Seen      int
	Status    string
	MediaType string
	AirStatus string
	StartDate string
}

type malListPage struct {
	Data []struct {
		Node struct {
			ID                int    `json:"id"`
			Title             string `json:"title"`
			NumEpisodes       int    `json:"num_episodes"`
			MediaType         string `json:"media_type"`
			Status            string `json:"status"`
			StartDate         string `json:"start_date"`
			AlternativeTitles struct {
				Synonyms []string `json:"synonyms"`
				English  string   `json:"en"`
				Japanese string   `json:"ja"`
			} `json:"alternative_titles"`
		} `json:"node"`
		ListStatus struct {
			Status             string `json:"status"`
			NumEpisodesWatched int    `json:"num_episodes_watched"`
		} `json:"list_status"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

type animeAV1SearchItem struct {
	ID    IDString `json:"id"`
	Title string   `json:"title"`
	Slug  string   `json:"slug"`
}

type animeAV1LibraryUpdate struct {
	MediaID IDString `json:"mediaId"`
	Status  *int     `json:"status,omitempty"`
	Episode *int     `json:"episode,omitempty"`
}

// avStatusFromMAL usa el mismo mapa que ya aplica la sincronización directa,
// pero en sentido contrario.
func avStatusFromMAL(status string) (int, error) {
	switch status {
	case "watching":
		return 0, nil
	case "plan_to_watch":
		return 1, nil
	case "completed":
		return 2, nil
	case "on_hold":
		return 3, nil
	case "dropped":
		return 4, nil
	default:
		return 0, fmt.Errorf("estado MAL no soportado: %s", status)
	}
}

func (a *App) fetchMALList(ctx context.Context) ([]MALListItem, error) {
	const fields = "list_status,num_episodes,media_type,alternative_titles,status,start_date"
	offset := 0
	out := make([]MALListItem, 0, 256)

	for {
		path := fmt.Sprintf("/users/@me/animelist?fields=%s&limit=1000&offset=%d", url.QueryEscape(fields), offset)
		var page malListPage
		if err := a.malRequestContext(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		for _, row := range page.Data {
			aliases := make([]string, 0, len(row.Node.AlternativeTitles.Synonyms)+2)
			if row.Node.AlternativeTitles.English != "" {
				aliases = append(aliases, row.Node.AlternativeTitles.English)
			}
			if row.Node.AlternativeTitles.Japanese != "" {
				aliases = append(aliases, row.Node.AlternativeTitles.Japanese)
			}
			aliases = append(aliases, row.Node.AlternativeTitles.Synonyms...)
			out = append(out, MALListItem{
				ID:        row.Node.ID,
				Title:     row.Node.Title,
				Aliases:   aliases,
				Episodes:  row.Node.NumEpisodes,
				Seen:      row.ListStatus.NumEpisodesWatched,
				Status:    row.ListStatus.Status,
				MediaType: row.Node.MediaType,
				AirStatus: row.Node.Status,
				StartDate: row.Node.StartDate,
			})
		}
		if page.Paging.Next == "" || len(page.Data) == 0 {
			break
		}
		offset += len(page.Data)
	}
	return out, nil
}

func (a *App) animeAV1Search(ctx context.Context, cookie, query string) ([]animeAV1SearchItem, error) {
	payload, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://animeav1.com/api/search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	browserHeaders(req, cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AnimeAV1 search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var items []animeAV1SearchItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("respuesta de búsqueda AnimeAV1 no válida: %w", err)
	}
	return items, nil
}

func (a *App) animeAV1UpdateStatus(ctx context.Context, cookie string, mediaID IDString, status int) error {
	payload, _ := json.Marshal(animeAV1LibraryUpdate{MediaID: mediaID, Status: &status})
	return a.animeAV1JSONPost(ctx, cookie, "https://animeav1.com/api/user/library", payload)
}

// animeAV1UpdateEpisode queda deliberadamente aislado porque AnimeAV1 usa un
// formulario SvelteKit para editar progreso, puntuación y notas. Necesitamos una
// captura HAR de un cambio de episodio para reproducir exactamente ese POST y no
// arriesgar datos de la biblioteca.
func (a *App) animeAV1UpdateEpisode(ctx context.Context, cookie string, mediaID IDString, episode int) error {
	return fmt.Errorf("actualización de episodio pendiente de confirmar con HAR: mediaId=%s episode=%d", mediaID, episode)
}

func (a *App) animeAV1JSONPost(ctx context.Context, cookie, endpoint string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	browserHeaders(req, cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("AnimeAV1 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// cachedMediaIDForMAL reutiliza la caché ya validada por la sincronización
// AnimeAV1 -> MAL. Esto evita volver a resolver títulos cuando la relación ya es
// conocida y es la vía preferente de la sincronización inversa.
func (a *App) cachedMediaIDForMAL(malID int) (IDString, bool) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	for _, entry := range a.cache {
		if entry.MALID == malID || entry.MALID2 == malID {
			return entry.MediaID, true
		}
	}
	return "", false
}

func bestAnimeAV1SearchMatch(item MALListItem, candidates []animeAV1SearchItem) (animeAV1SearchItem, int) {
	bestScore := -1
	var best animeAV1SearchItem
	sources := append([]string{item.Title}, item.Aliases...)
	for _, candidate := range candidates {
		for _, source := range sources {
			score := similarity(source, candidate.Title)
			if score > bestScore {
				bestScore = score
				best = candidate
			}
		}
	}
	return best, bestScore
}

func (a *App) resolveAnimeAV1Media(ctx context.Context, cookie string, item MALListItem) (IDString, string, int, error) {
	if mediaID, ok := a.cachedMediaIDForMAL(item.ID); ok {
		return mediaID, "cache", 999, nil
	}
	queries := append([]string{item.Title}, item.Aliases...)
	seen := map[string]bool{}
	bestScore := -1
	var best animeAV1SearchItem
	for _, query := range queries {
		key := normalize(query)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		items, err := a.animeAV1Search(ctx, cookie, query)
		if err != nil {
			continue
		}
		candidate, score := bestAnimeAV1SearchMatch(item, items)
		if candidate.ID != "" && score > bestScore {
			best, bestScore = candidate, score
		}
		if bestScore >= 100 {
			break
		}
	}
	threshold := getenvInt("REVERSE_TITLE_MATCH_THRESHOLD", 92)
	if best.ID == "" || bestScore < threshold {
		return "", "", bestScore, fmt.Errorf("sin coincidencia segura en AnimeAV1 para %q (%d puntos)", item.Title, bestScore)
	}
	return best.ID, best.Title, bestScore, nil
}

func reverseProgressMessage(item MALListItem, mediaID IDString, status int) string {
	return "MAL #" + strconv.Itoa(item.ID) + " -> AnimeAV1 " + string(mediaID) + " · " + item.Status + " -> " + strconv.Itoa(status) + " · episodio " + strconv.Itoa(item.Seen)
}
