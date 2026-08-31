module github.com/deadship2003/Panoxy

go 1.23.4

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
)

// mihomo 内核以 subtree 形式内嵌于 third_party,进程内复用其透明代理能力。
replace github.com/metacubex/mihomo => ./third_party/mihomo
