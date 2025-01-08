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
