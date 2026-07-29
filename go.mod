module github.com/a448582655/vibe-proxy

go 1.25.0

require (
	github.com/danlock/gogosseract v0.0.11-0ad3421.0.20250623141706-2521da518be1
	github.com/google/uuid v1.6.0
	github.com/prometheus/client_golang v1.19.1
	golang.org/x/crypto v0.53.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.55.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/danlock/pkg v0.0.18-fc7c42d // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/jerbob92/wazero-emscripten-embind v1.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.48.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tetratelabs/wazero v1.7.3 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// gogosseract's current Emscripten host bindings are incompatible with wazero
// v1.8.0 and newer. Keep this explicit until the built-in engine is upgraded.
replace github.com/tetratelabs/wazero => github.com/tetratelabs/wazero v1.7.3
