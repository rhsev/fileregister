// Package index is the fileregister core: the JSONL index (identity &
// membership truth), the bookmark database, and the fileanchor engine client
// for macOS metadata. It is deliberately self-contained — the Markdown
// annotation layer and the CLI live above it and import it, never the other
// way around. That direction is the architectural seam: nothing in here may
// know about Markdown surgery.
package index
