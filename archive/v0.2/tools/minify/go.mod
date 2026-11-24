module github.com/kitwork/engine2/tools/minify

go 1.26.0

require (
	github.com/kitwork/engine2 v0.0.0
	github.com/tdewolff/minify/v2 v2.24.13
)

require github.com/tdewolff/parse/v2 v2.8.12 // indirect

replace github.com/kitwork/engine2 => ../..

replace github.com/kitwork/engine => ../../../engine
