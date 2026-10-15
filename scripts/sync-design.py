from pathlib import Path
root = Path(__file__).resolve().parents[2]
source = (root / 'common/web/style.css').read_text()
tokens = source[source.index(':root{'):source.index('*{box-sizing')]
(root / 'platform/frontend/src/device-tokens.css').write_text('/* Synced from common/web/style.css by platform/scripts/sync-design.py. */\n' + tokens)
