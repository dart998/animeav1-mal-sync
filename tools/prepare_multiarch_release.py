from pathlib import Path

workflow = Path('.github/workflows/docker-publish.yml')
s = workflow.read_text(encoding='utf-8')
s = s.replace('platforms: linux/arm/v7', 'platforms: linux/arm/v7,linux/arm64,linux/amd64')
s = s.replace('Build and push ARMv7 image', 'Build and push multi-architecture image')
workflow.write_text(s, encoding='utf-8')

version = Path('VERSION')
version.write_text('1.7.0\n', encoding='utf-8')

main = Path('main.go')
s = main.read_text(encoding='utf-8')
s = s.replace('appVersion = "1.6.1"', 'appVersion = "1.7.0"', 1)
if 'appVersion = "1.7.0"' not in s:
    raise SystemExit('main appVersion anchor not found')
main.write_text(s, encoding='utf-8')

compose = Path('docker-compose.portainer.yml')
s = compose.read_text(encoding='utf-8')
s = s.replace('ovelayos/animeav1-mal-sync:1.6.1', 'ovelayos/animeav1-mal-sync:1.7.0', 1)
if 'ovelayos/animeav1-mal-sync:1.7.0' not in s:
    raise SystemExit('compose image anchor not found')
compose.write_text(s, encoding='utf-8')
