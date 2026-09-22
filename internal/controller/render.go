package controller

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/clbs-io/cloudflare-tunnel-ingress-controller/internal/tunnel"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Reasons of the Warning Events emitted for an Ingress.
const (
	// ReasonRuleSkipped: a path cannot be published (not a Service backend,
	// unknown named port, invalid regex or missing pathType).
	ReasonRuleSkipped = "RuleSkipped"
	// ReasonRuleConflict: the host and path are already served by an older
	// Ingress, or the host is reserved for the Kubernetes API tunnel.
	ReasonRuleConflict = "RuleConflict"
	// ReasonInvalidAnnotation: an annotation value does not parse or is not
	// supported; the setting keeps its default.
	ReasonInvalidAnnotation = "InvalidAnnotation"
	// ReasonUnsupported: the Ingress uses a field the tunnel cannot express.
	ReasonUnsupported = "Unsupported"
	// ReasonDNSConflict: a DNS record that does not point to the tunnel holds
	// the hostname, so the hostname is left out of the Ingress status.
	ReasonDNSConflict = "DNSConflict"
)

// warning is one problem found in an Ingress, emitted as a Warning Event.
type warning struct {
	Reason  string
	Message string
}

// ingressResult is what rendering produced for one Ingress.
type ingressResult struct {
	// Hostnames with at least one published rule, sorted
	Hostnames []string
	Warnings  []warning
}

func (r *ingressResult) warn(reason, format string, args ...any) {
	r.Warnings = append(r.Warnings, warning{Reason: reason, Message: fmt.Sprintf(format, args...)})
}

// renderResult is the desired tunnel state rendered from the active Ingresses.
type renderResult struct {
	// Rules in cloudflared match order, without the catch-all
	Rules tunnel.IngressRecords
	// AccessAppRequests maps hostnames to the Access application name to create
	AccessAppRequests map[string]string
	// Results per Ingress UID
	Results map[types.UID]*ingressResult
}

// portLookup resolves a named Service port to its number.
type portLookup func(namespace, service, port string) (int32, error)

// hostClass orders rules by how specific their hostname is. cloudflared uses
// the first matching rule, so exact hosts must precede wildcards and host-less
// rules must come last, or a broader rule would shadow a narrower one.
type hostClass int

const (
	hostExact hostClass = iota
	hostWildcard
	hostNone
)

// kubernetesApiRuleOwner names the Kubernetes API rule in conflict warnings.
const kubernetesApiRuleOwner = "the Kubernetes API tunnel"

// candidate is a rule before conflict resolution and ordering.
type candidate struct {
	record *zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress
	// result of the owning Ingress; nil for the Kubernetes API rule
	result *ingressResult
	// owner is the Ingress's namespace/name, for conflict messages
	owner string
	// Kubernetes path, for messages
	path string
	// reservesHost drops every other candidate on the same hostname
	reservesHost bool
	// class, labels, exact and length are the specificity sort keys, see
	// compareCandidates
	class  hostClass
	labels int
	exact  bool
	length int
	// precedence of the owner and position within it, lower first
	rank     int
	position int
}

// render translates the active Ingresses and the Kubernetes API tunnel into
// tunnel rules in cloudflared match order. Content that cannot be published
// is skipped and reported as a warning on its Ingress.
func render(ingresses []networkingv1.Ingress, kubeAPI KubernetesApiTunnelConfig, lookup portLookup) renderResult {
	out := renderResult{
		Rules:             make(tunnel.IngressRecords, 0),
		AccessAppRequests: make(map[string]string),
		Results:           make(map[types.UID]*ingressResult, len(ingresses)),
	}

	// The Kubernetes API rule goes first with rank -1, so it wins every
	// conflict; it also reserves its hostname.
	candidates := make([]candidate, 0)
	if kubeAPI.Enabled {
		class, labels := classifyHost(kubeAPI.Domain)
		candidates = append(candidates, candidate{
			record: &zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
				Hostname: kubeAPI.Domain,
				Service:  kubeAPI.GetService(),
				OriginRequest: zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{
					ProxyType: socksProxyType,
				},
			},
			owner:        kubernetesApiRuleOwner,
			reservesHost: true,
			class:        class,
			labels:       labels,
			rank:         -1,
		})
		out.AccessAppRequests[kubeAPI.Domain] = kubeAPI.CloudflareAccessAppName
	}

	// Candidates are appended in precedence order, oldest Ingress first and
	// then spec order, because resolveConflicts keeps the first candidate for
	// a host and path.
	ordered := slices.Clone(ingresses)
	slices.SortStableFunc(ordered, compareIngressPrecedence)

	for rank := range ordered {
		ingress := &ordered[rank]
		result := &ingressResult{}
		out.Results[ingress.UID] = result
		candidates = append(candidates, ingressCandidates(ingress, rank, result, lookup)...)
	}

	for _, c := range resolveConflicts(candidates) {
		out.Rules = append(out.Rules, c.record)
		if c.result != nil && len(c.record.Hostname) > 0 && !slices.Contains(c.result.Hostnames, c.record.Hostname) {
			c.result.Hostnames = append(c.result.Hostnames, c.record.Hostname)
		}
	}

	for i := range ordered {
		ingress := &ordered[i]
		result := out.Results[ingress.UID]
		slices.Sort(result.Hostnames)
		app_name := ingress.Annotations[AnnotationAccessAppName]
		if len(app_name) == 0 {
			continue
		}
		for _, hostname := range result.Hostnames {
			if _, ok := out.AccessAppRequests[hostname]; !ok {
				out.AccessAppRequests[hostname] = app_name
			}
		}
	}

	return out
}

// ingressCandidates turns the paths of one Ingress into candidates. The
// backend scheme and origin settings come from the Ingress annotations and
// apply to all of its rules. Paths that cannot be published are skipped with
// a warning on result.
func ingressCandidates(ingress *networkingv1.Ingress, rank int, result *ingressResult, lookup portLookup) []candidate {
	owner := ingress.Namespace + "/" + ingress.Name

	if ingress.Spec.DefaultBackend != nil {
		result.warn(ReasonUnsupported, "spec.defaultBackend is not supported, use rules")
	}

	scheme, scheme_warnings := backendScheme(ingress.Annotations)
	result.Warnings = append(result.Warnings, scheme_warnings...)
	origin := zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngressOriginRequest{}
	result.Warnings = append(result.Warnings, applyOriginRequestAnnotations(&origin, ingress.Annotations)...)

	candidates := make([]candidate, 0)
	position := 0
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			position++

			backend := path.Backend.Service
			if backend == nil {
				result.warn(ReasonRuleSkipped, "host %q path %q: only service backends are supported", rule.Host, path.Path)
				continue
			}
			port := backend.Port.Number
			if len(backend.Port.Name) > 0 {
				number, err := lookup(ingress.Namespace, backend.Name, backend.Port.Name)
				if err != nil {
					result.warn(ReasonRuleSkipped, "host %q path %q: %v", rule.Host, path.Path, err)
					continue
				}
				port = number
			}
			if path.PathType == nil {
				result.warn(ReasonRuleSkipped, "host %q path %q: pathType is required", rule.Host, path.Path)
				continue
			}
			cf_path, exact, length, err := translatePath(*path.PathType, path.Path, len(rule.Host) == 0)
			if err != nil {
				result.warn(ReasonRuleSkipped, "host %q path %q: %v", rule.Host, path.Path, err)
				continue
			}

			class, labels := classifyHost(rule.Host)
			candidates = append(candidates, candidate{
				record: &zero_trust.TunnelCloudflaredConfigurationGetResponseConfigIngress{
					Hostname:      rule.Host,
					Path:          cf_path,
					Service:       fmt.Sprintf("%s://%s.%s:%d", scheme, backend.Name, ingress.Namespace, port),
					OriginRequest: origin,
				},
				result:   result,
				owner:    owner,
				path:     path.Path,
				class:    class,
				labels:   labels,
				exact:    exact,
				length:   length,
				rank:     rank,
				position: position,
			})
		}
	}
	return candidates
}

// translatePath converts a Kubernetes path to a cloudflared path regex. It
// reports whether the match is exact and the path length used for ordering.
func translatePath(pathType networkingv1.PathType, path string, hostless bool) (string, bool, int, error) {
	switch pathType {
	case networkingv1.PathTypeExact:
		return "^" + regexp.QuoteMeta(path) + "$", true, len(path), nil
	case networkingv1.PathTypePrefix:
		prefix := strings.TrimRight(path, "/")
		if len(prefix) == 0 {
			return matchAllPath(hostless), false, 0, nil
		}
		return "^" + regexp.QuoteMeta(prefix) + "(/.*)?$", false, len(prefix), nil
	case networkingv1.PathTypeImplementationSpecific:
		// These match every path, like Prefix "/", and must conflict with it
		switch path {
		case "", "/", "^/":
			return matchAllPath(hostless), false, 0, nil
		}
		if _, err := regexp.Compile(path); err != nil {
			return "", false, 0, fmt.Errorf("invalid path regex %q: %w", path, err)
		}
		return path, false, len(path), nil
	default:
		return "", false, 0, fmt.Errorf("unsupported pathType %q", pathType)
	}
}

// matchAllPath matches every path. A rule without host and path is a
// catch-all, which cloudflared accepts only as the last rule, so host-less
// rules use a path that matches everything instead.
func matchAllPath(hostless bool) string {
	if hostless {
		return "^/"
	}
	return ""
}

// classifyHost returns the class of a hostname and its number of DNS labels.
// The "*" of a wildcard counts as a label, so *.a.example.com (4) sorts ahead
// of *.example.com (3).
func classifyHost(hostname string) (hostClass, int) {
	switch {
	case len(hostname) == 0:
		return hostNone, 0
	case strings.HasPrefix(hostname, "*."):
		return hostWildcard, strings.Count(hostname, ".") + 1
	default:
		return hostExact, strings.Count(hostname, ".") + 1
	}
}

// resolveConflicts drops candidates on a hostname reserved by another
// candidate, keeps the first candidate, in precedence order, for every
// hostname and path, warns the owners of the dropped ones, and orders the kept
// candidates for cloudflared first-match.
func resolveConflicts(candidates []candidate) []candidate {
	reserved := make(map[string]candidate)
	for _, c := range candidates {
		if c.reservesHost {
			reserved[c.record.Hostname] = c
		}
	}

	winners := make(map[[2]string]candidate, len(candidates))
	kept := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		if owner, ok := reserved[c.record.Hostname]; ok && !c.reservesHost {
			if c.result != nil {
				c.result.warn(ReasonRuleConflict, "host %q path %q: the host is reserved for %s", c.record.Hostname, c.path, owner.owner)
			}
			continue
		}
		key := [2]string{c.record.Hostname, c.record.Path}
		if winner, ok := winners[key]; ok {
			if c.result != nil {
				c.result.warn(ReasonRuleConflict, "host %q path %q is already served by %s", c.record.Hostname, c.path, winner.owner)
			}
			continue
		}
		winners[key] = c
		kept = append(kept, c)
	}
	slices.SortStableFunc(kept, compareCandidates)
	return kept
}

// compareCandidates orders candidates for cloudflared's first match, most
// specific first: exact hosts, then wildcards (more labels first), then
// host-less rules; by hostname so each host's rules stay together; Exact
// paths before others, and longer paths first, which gives Kubernetes path
// precedence; ties go to the older Ingress and the earlier path in its spec.
func compareCandidates(a, b candidate) int {
	return cmp.Or(
		cmp.Compare(a.class, b.class),
		cmp.Compare(b.labels, a.labels),
		strings.Compare(a.record.Hostname, b.record.Hostname),
		cmp.Compare(exactRank(a.exact), exactRank(b.exact)),
		cmp.Compare(b.length, a.length),
		cmp.Compare(a.rank, b.rank),
		cmp.Compare(a.position, b.position),
	)
}

// exactRank sorts exact path matches first.
func exactRank(exact bool) int {
	if exact {
		return 0
	}
	return 1
}

// compareIngressPrecedence orders Ingresses oldest first, then by namespace
// and name; the first one wins a host and path conflict.
func compareIngressPrecedence(a, b networkingv1.Ingress) int {
	return cmp.Or(
		a.CreationTimestamp.Compare(b.CreationTimestamp.Time),
		strings.Compare(a.Namespace, b.Namespace),
		strings.Compare(a.Name, b.Name),
	)
}
