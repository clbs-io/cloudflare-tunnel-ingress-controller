package controller

// Ingress annotations. Each applies to every rule of the annotated Ingress.
// A value that does not parse is reported as an InvalidAnnotation Event and
// the setting keeps cloudflared's default. Durations are Go durations in whole
// seconds (the unit of the tunnel configuration), at least 1s, such as "30s".

// AnnotationBackendProtocol selects the scheme of the origin URL: HTTP (the
// default), HTTPS or TCP, case-insensitive. TCP routes are proxied over
// WebSocket and need `cloudflared access tcp` on the client side.
const AnnotationBackendProtocol = "cloudflare-tunnel-ingress-controller.clbs.io/backend-protocol"

// AnnotationBackendProtocolHTTP selects a plain HTTP origin.
const AnnotationBackendProtocolHTTP = "HTTP"

// AnnotationBackendProtocolHTTPS selects an HTTPS origin.
const AnnotationBackendProtocolHTTPS = "HTTPS"

// AnnotationBackendProtocolTCP selects a raw TCP origin.
const AnnotationBackendProtocolTCP = "TCP"

// SupportedBackendProtocols lists the values AnnotationBackendProtocol accepts.
var SupportedBackendProtocols = []string{AnnotationBackendProtocolHTTP, AnnotationBackendProtocolHTTPS, AnnotationBackendProtocolTCP}

// AnnotationOriginConnectTimeout bounds establishing the TCP connection to
// the origin, excluding the TLS handshake (duration).
const AnnotationOriginConnectTimeout = "cloudflare-tunnel-ingress-controller.clbs.io/origin-connect-timeout"

// AnnotationOriginTlsTimeout bounds the TLS handshake with an HTTPS origin
// (duration).
const AnnotationOriginTlsTimeout = "cloudflare-tunnel-ingress-controller.clbs.io/origin-tls-timeout"

// AnnotationOriginTcpKeepalive sets the TCP keepalive interval of origin
// connections (duration).
const AnnotationOriginTcpKeepalive = "cloudflare-tunnel-ingress-controller.clbs.io/origin-tcp-keepalive"

// AnnotationOriginNoHappyEyeballs disables the Happy Eyeballs IPv4/IPv6
// fallback when dialing the origin ("true" or "false").
const AnnotationOriginNoHappyEyeballs = "cloudflare-tunnel-ingress-controller.clbs.io/origin-no-happy-eyeballs"

// AnnotationOriginKeepaliveConnections caps the idle keepalive connections
// cloudflared keeps open to the origin (integer).
const AnnotationOriginKeepaliveConnections = "cloudflare-tunnel-ingress-controller.clbs.io/origin-keepalive-connections"

// AnnotationOriginKeepaliveTimeout is how long an idle keepalive connection
// to the origin is kept (duration).
const AnnotationOriginKeepaliveTimeout = "cloudflare-tunnel-ingress-controller.clbs.io/origin-keepalive-timeout"

// AnnotationOriginHttpHostHeader overrides the Host header sent to the origin.
const AnnotationOriginHttpHostHeader = "cloudflare-tunnel-ingress-controller.clbs.io/origin-http-host-header"

// AnnotationOriginServerName is the hostname expected on the origin's TLS
// certificate, for origins whose certificate does not name the Service.
const AnnotationOriginServerName = "cloudflare-tunnel-ingress-controller.clbs.io/origin-server-name"

// AnnotationOriginNoTlsVerify makes cloudflared accept any origin certificate,
// for example a self-signed one ("true" or "false").
const AnnotationOriginNoTlsVerify = "cloudflare-tunnel-ingress-controller.clbs.io/origin-no-tls-verify"

// AnnotationOriginDisableChunkedEncoding stops cloudflared from sending
// chunked request bodies, for origins that do not support them such as some
// WSGI servers ("true" or "false").
const AnnotationOriginDisableChunkedEncoding = "cloudflare-tunnel-ingress-controller.clbs.io/origin-disable-chunked-encoding"

// AnnotationOriginProxyType set to "socks" makes cloudflared run a SOCKS proxy
// for the route instead of forwarding to the Service directly.
const AnnotationOriginProxyType = "cloudflare-tunnel-ingress-controller.clbs.io/origin-proxy-type"

// AnnotationOriginHttp2Origin makes cloudflared speak HTTP/2 to the origin,
// which must then be an HTTPS origin ("true" or "false").
const AnnotationOriginHttp2Origin = "cloudflare-tunnel-ingress-controller.clbs.io/origin-http2origin"

// Cloudflare Access annotations. These make cloudflared itself validate the
// Access token on every request to the route, so traffic that did not pass
// the Access application is rejected even if the application is
// misconfigured or missing.

// AnnotationAccessRequired enables the Access token check ("true" or "false").
const AnnotationAccessRequired = "cloudflare-tunnel-ingress-controller.clbs.io/access-required"

// AnnotationAccessTeamName is the Access team, the <team> of
// <team>.cloudflareaccess.com, whose tokens are accepted.
const AnnotationAccessTeamName = "cloudflare-tunnel-ingress-controller.clbs.io/access-team-name"

// AnnotationAccessAudTag lists the audience (AUD) tags of the Access
// application, comma-separated.
const AnnotationAccessAudTag = "cloudflare-tunnel-ingress-controller.clbs.io/access-aud-tag"

// AnnotationAccessAppName creates an account-level, self-hosted Access
// application with this name for each published hostname of the Ingress that
// has none. The application gets no policies and is never deleted.
const AnnotationAccessAppName = "cloudflare-tunnel-ingress-controller.clbs.io/access-app-name"
