package main

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
