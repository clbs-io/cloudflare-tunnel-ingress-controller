package controller

const AnnotationBackendProtocol = "cloudflare-tunnel-ingress-controller.clbs.io/backend-protocol"

const AnnotationBackendProtocolHTTP = "HTTP"
const AnnotationBackendProtocolHTTPS = "HTTPS"
const AnnotationBackendProtocolTCP = "TCP"

var SupportedBackendProtocols = []string{AnnotationBackendProtocolHTTP, AnnotationBackendProtocolHTTPS, AnnotationBackendProtocolTCP}

const AnnotationOriginConnectTimeout = "cloudflare-tunnel-ingress-controller.clbs.io/origin-connect-timeout"
const AnnotationOriginTlsTimeout = "cloudflare-tunnel-ingress-controller.clbs.io/origin-tls-timeout"
const AnnotationOriginTcpKeepalive = "cloudflare-tunnel-ingress-controller.clbs.io/origin-tcp-keepalive"
const AnnotationOriginNoHappyEyeballs = "cloudflare-tunnel-ingress-controller.clbs.io/origin-no-happy-eyeballs"
const AnnotationOriginKeepaliveConnections = "cloudflare-tunnel-ingress-controller.clbs.io/origin-keepalive-connections"
const AnnotationOriginKeepaliveTimeout = "cloudflare-tunnel-ingress-controller.clbs.io/origin-keepalive-timeout"
const AnnotationOriginHttpHostHeader = "cloudflare-tunnel-ingress-controller.clbs.io/origin-http-host-header"
const AnnotationOriginServerName = "cloudflare-tunnel-ingress-controller.clbs.io/origin-server-name"
const AnnotationOriginNoTlsVerify = "cloudflare-tunnel-ingress-controller.clbs.io/origin-no-tls-verify"
const AnnotationOriginDisableChunkedEncoding = "cloudflare-tunnel-ingress-controller.clbs.io/origin-disable-chunked-encoding"
const AnnotationOriginProxyType = "cloudflare-tunnel-ingress-controller.clbs.io/origin-proxy-type"
const AnnotationOriginHttp2Origin = "cloudflare-tunnel-ingress-controller.clbs.io/origin-http2origin"

// Cloudflare Access annotations — link tunnel route to an existing Access application
const AnnotationAccessRequired = "cloudflare-tunnel-ingress-controller.clbs.io/access-required"
const AnnotationAccessTeamName = "cloudflare-tunnel-ingress-controller.clbs.io/access-team-name"
const AnnotationAccessAudTag = "cloudflare-tunnel-ingress-controller.clbs.io/access-aud-tag"

// Cloudflare Access annotation — auto-create a new Access application for this ingress hostname
const AnnotationAccessAppName = "cloudflare-tunnel-ingress-controller.clbs.io/access-app-name"
