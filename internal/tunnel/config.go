package tunnel

type Config struct {
	Ingresses []IngressConfig
}

type IngressConfig struct {
	Hostname string
	Path     string
	Service  string
}
