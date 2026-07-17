module github.com/openfluke/octo

go 1.22.5

require github.com/openfluke/welvet v0.0.0

require (
	github.com/eliben/go-sentencepiece v0.7.0 // indirect
	github.com/openfluke/webgpu v1.0.4 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/openfluke/welvet => ../

replace github.com/eliben/go-sentencepiece => ../third_party/go-sentencepiece
