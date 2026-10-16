from pathlib import Path
root = Path(__file__).resolve().parents[1]
source = (root / 'common/web/style.css').read_text()
tokens = source[source.index(':root{'):source.index('*{box-sizing')]
(root / 'frontend/src/device-tokens.css').write_text('/* Synced from common/web/style.css by scripts/sync-design.py. */\n' + tokens)
