# Settings of dmgbuild for the disk image. scripts/macos/package.sh gives the app, the
# background and the icon of the volume with -D. The size of the window and the centers of
# the icons agree with macos/art/dmg-background.swift. Change both together.
# ruff: noqa: F821 -- dmgbuild runs this file with `defines` in its globals.
import os.path

app = defines["app"]
app_name = os.path.basename(app)

files = [app]
symlinks = {"Applications": "/Applications"}
icon = defines["volume_icon"]
background = defines["background"]

format = "ULMO"
filesystem = "HFS+"

# The height has the 32 points of the title bar of Finder. The content is 660 by 400.
window_rect = ((200, 120), (660, 432))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
show_icon_preview = False

arrange_by = None
icon_size = 120
text_size = 13
label_pos = "bottom"
icon_locations = {
    app_name: (170, 210),
    "Applications": (490, 210),
}
