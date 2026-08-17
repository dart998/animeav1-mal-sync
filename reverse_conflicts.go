package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ReverseTruthAnimeAV1 = "animeav1"
	ReverseTruthMAL      = "mal"
)

// ReverseResolution guarda una decisión explícita del usuario para una serie cuyo
// progreso no se puede comparar 1:1 entre AnimeAV1 y MAL. La elección persiste y
// se considera fuente de verdad en sincronizaciones futuras.
type ReverseResolution struct {
	MediaID          IDString `json:"media_id"`
	MALID            int      `json:"mal_id"`
	AnimeAV1Title     string   `json:"animeav1_title"`
	MALTitle          string   `json:"mal_title"`
	PreferredSource   string   `json:"preferred_source"`
	AnimeAV1SeenAtSet int      `json:"animeav1_seen_at_set"`
	MALSeenAtSet      int      `json:"mal_seen_at_set"`
	UpdatedAt         int64    `json:"updated_at"`
}

func reverseResolutionKey(mediaID IDString, malID int) string {
	return string(mediaID) + ":" + fmt.Sprint(malID)
}

func (a *App) reverseResolutionPath() string {
	return filepath.Join(a.dataDir, "reverse_resolutions.json")
}

func (a *App) loadReverseResolutions() map[string]ReverseResolution {
	out := map[string]ReverseResolution{}
	b, err := os.ReadFile(a.reverseResolutionPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func (a *App) saveReverseResolutions(values map[string]ReverseResolution) error {
	b, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.reverseResolutionPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.reverseResolutionPath())
}

func (a *App) reverseResolution(mediaID IDString, malID int) (ReverseResolution, bool) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	values := a.loadReverseResolutions()
	v, ok := values[reverseResolutionKey(mediaID, malID)]
	return v, ok
}

func (a *App) setReverseResolution(v ReverseResolution) error {
	v.PreferredSource = strings.ToLower(strings.TrimSpace(v.PreferredSource))
	if v.MediaID == "" || v.MALID <= 0 {
		return errors.New("resolución sin IDs válidos")
	}
	if v.PreferredSource != ReverseTruthAnimeAV1 && v.PreferredSource != ReverseTruthMAL {
		return errors.New("fuente de verdad no válida")
	}
	v.UpdatedAt = time.Now().Unix()
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	values := a.loadReverseResolutions()
	values[reverseResolutionKey(v.MediaID, v.MALID)] = v
	return a.saveReverseResolutions(values)
}

// reverseEpisodeConflict determina si una discrepancia de progreso requiere
// intervención. Si ya existe una resolución persistente, nunca vuelve a preguntar.
func (a *App) reverseEpisodeConflict(av AVItem, mal MALListItem) (ReverseResolution, bool) {
	if av.Seen == mal.Seen {
		return ReverseResolution{}, false
	}
	if saved, ok := a.reverseResolution(av.MediaID, mal.ID); ok {
		return saved, false
	}
	return ReverseResolution{
		MediaID:          av.MediaID,
		MALID:            mal.ID,
		AnimeAV1Title:     av.Title,
		MALTitle:          mal.Title,
		AnimeAV1SeenAtSet: av.Seen,
		MALSeenAtSet:      mal.Seen,
	}, true
}

// reverseTruthPolicy explica qué hacer con el progreso cuando existe una decisión
// persistente. En ningún caso la sincronización inversa escribe episodios en AV1.
func reverseTruthPolicy(v ReverseResolution) string {
	if v.PreferredSource == ReverseTruthAnimeAV1 {
		return "animeav1"
	}
	if v.PreferredSource == ReverseTruthMAL {
		return "mal"
	}
	return "unresolved"
}
