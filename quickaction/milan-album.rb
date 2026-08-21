#!/usr/bin/env ruby
# frozen_string_literal: true

# milan script: a fileregister album straight in the browser — one link is enough.
#
#   http://localhost:8080/album/<binder>        → album HTML (renders on first call)
#   http://localhost:8080/album/<binder>/fresh  → re-render first, then show
#   …/stream/album/<binder>/fresh               → re-render without the 5s timeout
#                                                 (for large albums)
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
abort "Usage: album/<binder>[/fresh]" if binder.to_s.empty?

bin = ENV["REGISTER_BIN"] || File.expand_path("~/bin/register")
abort "register not found: #{bin}" unless File.executable?(bin)

dir    = ENV["ALBUM_MILAN_DIR"] || File.join(notes_dir, "collections", "albums", "milan")
source = ENV["ALBUM_MILAN_SOURCE"] || "alben"
# Slug exactly as the renderer builds it (marshal_sanitize + album_asset_safe).
slug   = binder.strip.gsub(%r{[/\\\0\n\r]}, "-").gsub(/[^\w. -]/, "-")
html   = File.join(dir, "#{slug}.html")

if mode == "fresh" || !File.exist?(html)
  system(bin, "album", binder, "--milan", dir, out: File::NULL) or
    abort "register album failed for '#{binder}'"
end
abort "album '#{binder}' not found" unless File.exist?(html)

# Point relative assets at milan's notes route (the same thing Stage does).
puts File.read(html, encoding: "utf-8")
         .gsub(%r{\b(src|href)="(images/[^"]+)"}) { %(#{$1}="/notes/#{source}/assets/#{$2}") }
