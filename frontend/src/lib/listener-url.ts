const defaultDataPlaneOrigin = "http://127.0.0.1:8080"

function withoutIPv6Brackets(hostname: string): string {
  if (hostname.startsWith("[") && hostname.endsWith("]")) {
    return hostname.slice(1, -1)
  }
  return hostname
}

export function dataPlaneOrigin(listen: string | undefined, browserHostname: string): string {
  if (!listen) return defaultDataPlaneOrigin

  let host = ""
  let port = ""
  if (listen.startsWith("[")) {
    const closingBracket = listen.indexOf("]")
    if (closingBracket < 0 || listen[closingBracket + 1] !== ":") return defaultDataPlaneOrigin
    host = listen.slice(1, closingBracket)
    port = listen.slice(closingBracket + 2)
  } else {
    const separator = listen.lastIndexOf(":")
    if (separator < 0) return defaultDataPlaneOrigin
    host = listen.slice(0, separator)
    port = listen.slice(separator + 1)
  }
  if (!/^\d+$/.test(port)) return defaultDataPlaneOrigin

  if (!host || host === "0.0.0.0" || host === "::") {
    const localHost = withoutIPv6Brackets(browserHostname) || "127.0.0.1"
    // The control plane is loopback-only. When an IPv6 control URL is used
    // alongside an IPv4-only data wildcard, use the matching IPv4 loopback
    // rather than emitting an unreachable IPv6 endpoint.
    host = host === "0.0.0.0" && localHost.includes(":") ? "127.0.0.1" : localHost
  }

  host = withoutIPv6Brackets(host)
  const formattedHost = host.includes(":") ? `[${host}]` : host
  return `http://${formattedHost}:${port}`
}
