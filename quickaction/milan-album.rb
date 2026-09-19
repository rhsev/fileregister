#!/usr/bin/env ruby
# frozen_string_literal: true

# milan script: a fileregister album straight in the browser — one link is enough.
#
#   http://localhost:8080/album/<binder>        → album HTML (renders on first call)
#   http://localhost:8080/album/                → the first album, alphabetically
#   http://localhost:8080/album/_note/<b>/<f>   → a markdown attachment, rendered
#   http://localhost:8080/album/<binder>/fresh  → re-render first, then show
#   …/stream/album/<binder>/fresh               → re-render without the 5s timeout
#                                                 (for large albums)
#
# Above the grid sits a chip row over every album already rendered into the
# milan directory, so one panel browses them all. Links are relative — the row
# works behind dylan's instance prefix as well as against milan directly, and so
# does the back link out of a note view.
#
# Markdown attachments are rendered rather than handed over as a download, the
# way the pinboard renders a note body (apex, `--mode unified`, overridable via
# ALBUM_MD/APEX_BIN; raw source in a <pre> if that fails). Images referenced
# from inside such a note are not resolved — the album copies the attachment,
# not whatever the attachment points at.
#
# Install:  ln -s <repo>/quickaction/milan-album.rb <mi.lan>/scripts/custom/album.rb
# Asset paths are rewritten onto milan's notes route; the source id comes from
# ALBUM_MILAN_SOURCE (default "alben"), the render directory from ALBUM_MILAN_DIR
# (default <notes>/collections/albums/milan).
# Environment as for the Quick Actions: ~/.config/fileregister/quickaction.env.

require "yaml"
require "erb"
require "open3"

# milan runs under launchd with a bare environment — no LANG, so the default
# external encoding is US-ASCII. Without this, every comparison against text
# read from a subprocess raises Encoding::CompatibilityError. pinboard.rb sets
# the same line for the same reason.
Encoding.default_external = Encoding::UTF_8

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

def album_esc(s) = s.to_s.gsub("&", "&amp;").gsub("<", "&lt;").gsub(">", "&gt;").gsub('"', "&quot;")

# milan's PATH has neither ~/bin nor /opt/homebrew/bin, so a bare name fails.
def find_bin(name, env)
  return ENV[env] if ENV[env].to_s != ""
  ["~/bin/#{name}", "/opt/homebrew/bin/#{name}", "/usr/local/bin/#{name}"].each do |c|
    path = File.expand_path(c)
    return path if File.executable?(path)
  end
  name
end

# Same renderer the pinboard uses. `unified` parses YAML frontmatter instead of
# dumping it into the page the way gfm would — album attachments are the user's
# own notes and often carry one. `--hardbreaks` keeps every newline a newline;
# a list typed without blank lines should not come back as one run-on line.
def render_markdown(md)
  cmd = ENV["ALBUM_MD"].to_s != "" ? ENV["ALBUM_MD"].split : [find_bin("apex", "APEX_BIN"), "--mode", "unified", "--hardbreaks"]
  out, status = Open3.capture2(*cmd, stdin_data: md, err: File::NULL)
  # capture2 tags the bytes with the external encoding, whatever that happens to
  # be; apex emits UTF-8. Saying so here keeps the check below from raising —
  # and the rescue from quietly turning a rendered note into raw source.
  out = out.force_encoding(Encoding::UTF_8)
  return out if status.success? && !out.strip.empty?
rescue StandardError
  nil
end

def note_href(binder, file)
  "_note/#{ERB::Util.url_encode(binder)}/#{ERB::Util.url_encode(file)}"
end

# A markdown attachment rendered in the album's own visual language, with the
# way back. Reached as _note/<binder>/<file>; `../../<binder>` lands on the
# album again, behind dylan's prefix as well as straight on milan.
def note_page(binder, file, dir)
  path = File.join(dir, "images", File.basename(file.to_s))   # basename: no traversal
  body =
    if File.file?(path)
      src = File.read(path, encoding: "utf-8")
      render_markdown(src) || "<pre>#{album_esc(src)}</pre>"
    else
      "<p>Note not found.</p>"
    end

  <<~HTML
    <!DOCTYPE html>
    <html lang="de">
    <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>#{album_esc(File.basename(file.to_s))}</title>
    <style>
    body.album-page { margin: 0; background: Canvas; }
    .album { color-scheme: light dark; font-family: -apple-system, sans-serif;
             max-width: 48rem; margin: 0 auto; padding: 2rem 1rem; color: CanvasText;
             line-height: 1.6; }
    .album .back { display: inline-block; margin-bottom: 1.4rem; font-size: .85rem;
             text-decoration: none; color: CanvasText; opacity: .7;
             border: 1px solid color-mix(in srgb, CanvasText 20%, transparent);
             border-radius: 6px; padding: .2rem .7rem; }
    .album .back:hover { opacity: 1; }
    .album h1 { font-weight: 600; letter-spacing: .02em; font-size: 1.4rem; }
    .album img { max-width: 100%; height: auto; border-radius: 6px; }
    .album pre { overflow-x: auto; padding: .8rem 1rem; border-radius: 6px;
             background: color-mix(in srgb, CanvasText 7%, transparent); }
    .album code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    .album blockquote { margin: 0; padding-left: 1rem;
             border-left: 3px solid color-mix(in srgb, CanvasText 25%, transparent); }
    .album table { border-collapse: collapse; }
    .album th, .album td { border: 1px solid color-mix(in srgb, CanvasText 20%, transparent);
             padding: .3rem .6rem; text-align: left; }
    </style>
    </head>
    <body class="album-page">
    <div class="album">
    <a class="back" href="../../#{ERB::Util.url_encode(binder)}">&larr; #{album_esc(binder)}</a>
    <h1>#{album_esc(File.basename(file.to_s))}</h1>
    #{body}
    </div>
    </body>
    </html>
  HTML
end

# _note/<binder>/<file> — the markdown view, before anything tries to treat
# "_note" as a binder name.
if binder == "_note"
  note_binder, note_file = mode.to_s.split("/", 2)
  abort "Usage: album/_note/<binder>/<file>" if note_file.to_s.empty?
  print note_page(note_binder, note_file, dir)
  exit 0
end

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

# Markdown attachments go to the note view FIRST, while they still look like
# `images/x.md`. After this they match neither the asset rewrite nor the
# new-tab rule below, which is the point: a rendered note is a page of ours,
# so it opens in the frame and offers the way back, while a PDF or an image
# is a file and gets its own tab.
#
# Then the remaining assets move onto milan's notes route (the same thing Stage
# does). The chip row goes above the title — it is navigation, not content.
puts File.read(html, encoding: "utf-8")
         .gsub(%r{<a href="images/([^"]+\.(?:md|markdown))"}) { %(<a href="#{note_href(slug, $1)}") }
         .gsub(%r{\b(src|href)="(images/[^"]+)"}) { %(#{$1}="/notes/#{source}/assets/#{$2}") }
         .gsub(%r{<a href="(/notes/[^"]+)"}) { %(<a href="#{$1}" target="_blank" rel="noopener") }
         .sub("</style>", "#{NAV_CSS}</style>")
         .sub(%r{(<h1>)}) { "#{nav_html(albums, slug)}#{$1}" }
