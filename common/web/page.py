from pathlib import Path
ROOT = Path(__file__).resolve().parents[2]
def _source(path, name):
    """Shared fragment first, then a file sitting next to the page."""
    shared = ROOT / "common" / "web" / name
    if shared.is_file():
        return shared.read_text()
    return (path.parent / name).read_text()
def render_page(path):
    path = Path(path)
    text = path.read_text()
    # The product page lists its own fragments; a name resolves to common/web
    # first and to the product's web directory second.
    for name in ("style.css", "i18n.js", "service.js", "ui.js", "automation.js", "page.js"):
        content = _source(path, name)
        for marker in ("/* @include " + name + " */", "<!-- @include " + name + " -->"):
            text = text.replace(marker, content)
    # Apply the saved theme before first paint.
    appearance = (ROOT / "common" / "web" / "appearance.js").read_text()
    script = "<script>" + appearance + "</script>"
    text = text.replace("</head>", script + "</head>", 1) if "</head>" in text else text.replace("</html>", script + "</html>")
    product = path.resolve().relative_to(ROOT).parts[0]
    return text.encode("utf-8")
if __name__ == "__main__":
    import sys
    sys.stdout.buffer.write(render_page(sys.argv[1]))
