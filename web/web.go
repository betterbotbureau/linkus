// Package web embeds the pages the node ships with.
package web

import "embed"

// StarterFS holds the example site that `linknode example` writes into an
// empty public folder.
//
//go:embed starter
var StarterFS embed.FS
