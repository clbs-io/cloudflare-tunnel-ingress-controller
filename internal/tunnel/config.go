package tunnel

import (
	"time"
)

type Config struct {
	Ingresses []IngressConfig
}

type IngressConfig struct {
	Hostname     string
	Path         string
	Service      string
	OriginConfig *IngressOriginConfig
}

type IngressOriginConfig struct {
	// HTTP proxy timeout for establishing a new connection
	ConnectTimeout *time.Duration `json:"connectTimeout,omitempty"`
	// HTTP proxy timeout for completing a TLS handshake
	TLSTimeout *time.Duration `json:"tlsTimeout,omitempty"`
	// HTTP proxy TCP keepalive duration
	TCPKeepAlive *time.Duration `json:"tcpKeepAlive,omitempty"`
	// HTTP proxy should disable "happy eyeballs" for IPv4/v6 fallback
	NoHappyEyeballs *bool `json:"noHappyEyeballs,omitempty"`
	// HTTP proxy maximum keepalive connection pool size
	KeepAliveConnections *int `json:"keepAliveConnections,omitempty"`
	// HTTP proxy timeout for closing an idle connection
	KeepAliveTimeout *time.Duration `json:"keepAliveTimeout,omitempty"`
	// Sets the HTTP Host header for the local webserver.
	HTTPHostHeader *string `json:"httpHostHeader,omitempty"`
	// Hostname on the origin server certificate.
	OriginServerName *string `json:"originServerName,omitempty"`
	// Disables TLS verification of the certificate presented by your origin.
	// Will allow any certificate from the origin to be accepted.
	// Note: The connection from your machine to Cloudflare's Edge is still encrypted.
	NoTLSVerify *bool `json:"noTLSVerify,omitempty"`
	// Disables chunked transfer encoding.
	// Useful if you are running a WSGI server.
	DisableChunkedEncoding *bool `json:"disableChunkedEncoding,omitempty"`
	// Runs as jump host
	BastionMode *bool `json:"bastionMode,omitempty"`
	// Listen address for the proxy.
	ProxyAddress *string `json:"proxyAddress,omitempty"`
	// Listen port for the proxy.
	ProxyPort *uint `json:"proxyPort,omitempty"`
	// Valid options are 'socks' or empty.
	ProxyType *string `json:"proxyType,omitempty"`
	// Attempt to connect to origin with HTTP/2
	Http2Origin *bool `json:"http2Origin,omitempty"`
}
