# Repository assets

This directory holds the artwork and the metadata used on the project's GitHub
pages.

| File | Size | Purpose |
|---|---|---|
| `social-preview.png` | 1280×640 | GitHub Settings → Social preview. Also suitable for link previews. |
| `demo.png` | 1216×1056 | Terminal example at the top of `README.md`. |
| `logo.png` | 512×512 | Logo mark for the README, avatars, and future docs. |
| `generate-assets.py` | — | Reproducible generator for the three images. |

## Regenerating the images

The generator uses Pillow and Noto Sans / Noto Sans Mono:

```bash
python3 -m pip install --user Pillow
make assets            # or: python3 docs/generate-assets.py
```

Edit the copy and layout constants in `generate-assets.py`, run it, and inspect
the three files before committing. The terminal content mirrors real HiveServer2
output; keep it consistent with the CLI when output changes.

## Setting the social preview

GitHub does not expose social preview uploads through its REST API. Use the web
UI after the image is on `master`:

1. Open the repository on GitHub.
2. Go to **Settings → General → Social preview**.
3. Choose **Edit → Upload an image** and select `docs/social-preview.png`.

The recommended size is 1280×640, and the file must be under 1 MB. GitHub caches
previews, so existing shared links may keep the old image for a while.

## Setting the About panel

Use the script so the description, topics, and repository switches stay
consistent:

```bash
.github/scripts/setup-repo.sh [OWNER/REPO]
```

It sets:

- **Description:** the one-line positioning shown in search and the About panel.
- **Topics:** discoverability terms such as `hive`, `hiveserver2`, `golang`,
  `thrift`, and `cli`.
- **Discussions:** enabled for usage questions and design conversations.
- **Wiki:** disabled, because the README and `docs/` are the source of truth.
- **Private vulnerability reporting:** enabled so `SECURITY.md` has a working
  private channel.

The **Website** field is intentionally left empty until there is a real site to
point at. Do not point it at a placeholder URL.
