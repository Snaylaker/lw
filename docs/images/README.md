# Documentation screenshots

These screenshots are generated from the real exported TUI views with deterministic synthetic
data. No live provider, credential, or user repository is involved.

Install [Freeze v0.2.2](https://github.com/charmbracelet/freeze/releases/tag/v0.2.2), then run
from the repository root:

```sh
for screen in provider-search branch-name worktree-ready; do
  CLICOLOR_FORCE=1 go run ./scripts/screenshots --screen "$screen" |
    freeze --output "docs/images/$screen.png" --window --background '#0D1117' \
      --margin 24 --padding '28,36' --width 1000 --font.size 16 --line-height 1.3 \
      --border.radius 12 --shadow.blur 0
done
```

The renderer uses the normal Go build and exported TUI views instead of a separate mock UI.
Regenerate the images after changing TUI wording or layout.
