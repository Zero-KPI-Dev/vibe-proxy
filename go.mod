module github.com/a448582655/vibe-proxy

go 1.22

require (
	github.com/danlock/gogosseract v0.0.11-0ad3421.0.20250623141706-2521da518be1
	github.com/google/uuid v1.6.0
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/prometheus/client_golang v1.19.1
	golang.org/x/crypto v0.24.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/danlock/pkg v0.0.18-fc7c42d // indirect
	github.com/jerbob92/wazero-emscripten-embind v1.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.48.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/tetratelabs/wazero v1.7.3 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)

// gogosseract's current Emscripten host bindings are incompatible with wazero
// v1.8.0 and newer. Keep this explicit until the built-in engine is upgraded.
replace github.com/tetratelabs/wazero => github.com/tetratelabs/wazero v1.7.3
