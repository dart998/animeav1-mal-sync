from pathlib import Path

p = Path('main.go')
s = p.read_text(encoding='utf-8')
old = r'''fmt.Fprintf(w, `{\"status\":\"ok\",\"version\":%q}`, appVersion)'''
new = r'''fmt.Fprintf(w, `{"status":"ok","version":%q}`, appVersion)'''
if old not in s:
    raise SystemExit('health JSON anchor not found')
s = s.replace(old, new, 1)
p.write_text(s, encoding='utf-8')
