package controller

const AnnotationBackendProtocol = "cloudflare-tunnel-ingress-controller.clbs.io/backend-protocol"

const AnnotationBackendProtocolHTTP = "HTTP"

var SupportedBackendProtocols = []string{AnnotationBackendProtocolHTTP}
