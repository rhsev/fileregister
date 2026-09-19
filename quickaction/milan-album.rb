#!/usr/bin/env ruby
# frozen_string_literal: true

# milan script: a fileregister album straight in the browser — one link is enough.
#
#   http://localhost:8080/album/<binder>        → album HTML (renders on first call)
#   http://localhost:8080/album/                → the first album, alphabetically
#   http://localhost:8080/album/<binder>/fresh  → re-render first, then show
#   …/stream/album/<binder>/fresh               → re-render without the 5s timeout
#                                                 (for large albums)
#
# Above the grid sits a chip row over every album already rendered into the
# milan directory, so one panel browses them all. Links are relative — the row
# works behind dylan's instance prefix as well as against milan directly.
#
# Install:  ln -s <repo>/quickaction/milan-album.rb <mi.lan>/scripts/custom/album.rb
# Asset paths are rewritten onto milan's notes route; the source id comes from
# ALBUM_MILAN_SOURCE (default "alben"), the render directory from ALBUM_MILAN_DIR
# (default <notes>/collections/albums/milan).
# Environment as for the Quick Actions: ~/.config/fileregister/quickaction.env.

require "yaml"

env_file = File.expand_path("~/.config/fileregister/quickaction.env")
if File.exist?(env_file)
  File.readlines(env_file).each do |line|
    next unless (m = line.match(/\A\s*([A-Z_]+)=("?)(.*)\2\s*\z/))
    ENV[m[1]] = m[3].sub(/\A\$HOME/, Dir.home)
  end
end

def notes_dir
  return ENV["GRUBBER_NOTES"] if ENV["GRUBBER_NOTES"].to_s != ""
  set = ENV["GRUBBER_SET"].to_s
  abort "GRUBBER_SET/GRUBBER_NOTES missing (quickaction.env)" if set.empty?
  cfg_path = File.expand_path(ENV["GRUBBER_CONFIG"] || "~/.config/grubber/config.yaml")
  cfg  = YAML.safe_load(File.read(cfg_path, encoding: "utf-8"))
  path = cfg.dig("sets", set, "path") or abort "Set '#{set}' not found in the grubber config"
  File.expand_path(path)
end

binder, mode = ARGV[0].to_s.split("/", 2)

dir    = ENV["ALBUM_MILAN_DIR"] || File.join(notes_dir, "collections", "albums", "milan")
source = ENV["ALBUM_MILAN_SOURCE"] || "alben"

# Slug exactly as the renderer builds it (marshal_sanitize + album_asset_safe).
def album_slug(name) = name.strip.gsub(%r{[/\\\0\n\r]}, "-").gsub(/[^\w. -]/, "-")

# Every album already rendered into the milan directory, by slug. That is the
# set this script can serve without calling register, so it is also the set the
# chip row offers.
def rendered_albums(dir)
  Dir.glob(File.join(dir, "*.html")).map { |p| File.basename(p, ".html") }.sort
end

albums = rendered_albums(dir)
binder = albums.first.to_s if binder.to_s.empty?   # no binder → first album
abort "Usage: album/<binder>[/fresh]" if binder.to_s.empty?

slug = album_slug(binder)
html = File.join(dir, "#{slug}.html")

if mode == "fresh" || !File.exist?(html)
  bin = ENV["REGISTER_BIN"] || File.expand_path("~/bin/register")
  abort "register not found: #{bin}" unless File.executable?(bin)
  system(bin, "album", binder, "--milan", dir, out: File::NULL) or
    abort "register album failed for '#{binder}'"
  albums = rendered_albums(dir)                    # a fresh album joins the row
end
abort "album '#{binder}' not found" unless File.exist?(html)

# Chip row over the albums, in the album's own visual language (system colours,
# so it follows the page's light/dark scheme like everything else here).
NAV_CSS = <<~CSS
  .album .albums { display: flex; flex-wrap: wrap; gap: .4rem; margin: 0 0 1.2rem; }
  .album .albums a { border: 1px solid color-mix(in srgb, CanvasText 20%, transparent);
    border-radius: 6px; padding: .2rem .7rem; font-size: .85rem;
    text-decoration: none; color: CanvasText; opacity: .7; }
  .album .albums a:hover { opacity: 1; }
  .album .albums a.on { background: color-mix(in srgb, CanvasText 12%, transparent);
    border-color: color-mix(in srgb, CanvasText 45%, transparent); opacity: 1; }
CSS

def album_esc(s) = s.to_s.gsub("&", "&amp;").gsub("<", "&lt;").gsub(">", "&gt;").gsub('"', "&quot;")

# Links stay relative: from /album/<slug> as well as from /album/, a bare slug
# resolves to the sibling album. That keeps the row working both behind dylan's
# instance prefix and when milan is called directly — no base-URL script needed.
# Slugs never contain a slash (album_slug strips them), so there is no depth to
# get wrong.
def nav_html(albums, current)
  return "" if albums.length < 2
  chips = albums.map do |a|
    cls = a == current ? %( class="on") : ""
    %(<a#{cls} href="#{album_esc(a)}">#{album_esc(a)}</a>)
  end.join
  %(<nav class="albums">#{chips}</nav>\n)
end

# Point relative assets at milan's notes route (the same thing Stage does),
# then send every asset link to a new tab. Inside the stage the album is an
# iframe: a plain link would replace the panel with the PDF and leave no way
# back. The chip row goes above the title — it is navigation, not content.
puts File.read(html, encoding: "utf-8")
         .gsub(%r{\b(src|href)="(images/[^"]+)"}) { %(#{$1}="/notes/#{source}/assets/#{$2}") }
         .gsub(%r{<a href="(/notes/[^"]+)"}) { %(<a href="#{$1}" target="_blank" rel="noopener") }
         .sub("</style>", "#{NAV_CSS}</style>")
         .sub(%r{(<h1>)}) { "#{nav_html(albums, slug)}#{$1}" }
