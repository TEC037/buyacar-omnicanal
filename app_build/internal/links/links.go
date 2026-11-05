package links

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"buyacar.com.co/omnicanal/internal/channel"
)

// Repository gestiona enlaces editables sin recompilar, aplicando allowlist HTTPS.
type Repository struct {
	mu             sync.RWMutex
	allowedDomains map[string]bool
	links          map[string]channel.Link
}

func NewRepository(allowedDomains []string) *Repository {
	domains := make(map[string]bool, len(allowedDomains))
	for _, d := range allowedDomains {
		domains[strings.ToLower(strings.TrimSpace(d))] = true
	}
	return &Repository{
		allowedDomains: domains,
		links:          make(map[string]channel.Link),
	}
}

// Upsert valida que la URL sea HTTPS y pertenezca a la allowlist antes de guardarla.
func (r *Repository) Upsert(l channel.Link) error {
	parsed, err := url.Parse(l.URL)
	if err != nil || parsed.Scheme != "https" {
		return fmt.Errorf("links: invalid or non-HTTPS URL %q", l.URL)
	}
	host := strings.ToLower(parsed.Hostname())
	if !r.isAllowedHost(host) {
		return fmt.Errorf("links: domain %q is not in allowlist", host)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.links[l.ID] = l
	return nil
}

func (r *Repository) isAllowedHost(host string) bool {
	if r.allowedDomains[host] {
		return true
	}
	for allowed := range r.allowedDomains {
		if strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// ActiveLinks devuelve solo los enlaces activos para evitar visitar enlaces deshabilitados.
func (r *Repository) ActiveLinks() []channel.Link {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var active []channel.Link
	for _, l := range r.links {
		if l.Active {
			active = append(active, l)
		}
	}
	return active
}
