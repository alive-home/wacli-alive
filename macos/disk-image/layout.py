# dmgbuild settings for the disk image window. scripts/draw-disk-image.mjs
# builds a throwaway image with these only to take its .DS_Store; the release
# image (scripts/pack-mac.mjs) copies that file and background.tiff in beside
# wacli.app and the Applications link. The .DS_Store finds the background by
# the volume name, wacli, and its path, /.background.tiff. Finder measures
# window_rect from the bottom of the screen, and its height includes the
# title bar.
files = [defines["app"]]
symlinks = {"Applications": "/Applications"}
background = defines["background"]
window_rect = ((200, 368), (660, 432))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
icon_size = 128
text_size = 13
icon_locations = {"wacli.app": (170, 180), "Applications": (490, 180)}
