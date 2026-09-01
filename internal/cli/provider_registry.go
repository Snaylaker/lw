package cli

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/snaylaker/lw/internal/processenv"
	issueprovider "github.com/snaylaker/lw/provider"
)

var (
	providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	envNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

var builtInProviderNames = map[issueprovider.ID]string{
	issueprovider.Linear: "Linear",
	issueprovider.GitHub: "GitHub",
	issueprovider.Jira:   "Jira",
}

type providerRegistry struct {
	extensions map[issueprovider.ID]issueprovider.Provider
	sensitive  map[string]string
}

func newProviderRegistry(candidates []issueprovider.Provider) (*providerRegistry, error) {
	registry := &providerRegistry{
		extensions: make(map[issueprovider.ID]issueprovider.Provider, len(candidates)),
		sensitive:  make(map[string]string, len(processenv.BuiltInProviderSecrets())),
	}
	for _, name := range processenv.BuiltInProviderSecrets() {
		registry.sensitive[strings.ToLower(name)] = name
	}
	for index, candidate := range candidates {
		if nilProvider(candidate) {
			return nil, fmt.Errorf("provider extension %d is nil", index+1)
		}
		rawID := strings.TrimSpace(string(candidate.ID()))
		canonical := strings.ToLower(rawID)
		if rawID != canonical || !providerIDPattern.MatchString(canonical) {
			return nil, fmt.Errorf("provider extension ID %q must use lowercase letters, digits, hyphens, or underscores and start with a letter", rawID)
		}
		id := issueprovider.ID(canonical)
		if _, reserved := builtInProviderNames[id]; reserved {
			return nil, fmt.Errorf("provider extension ID %q is reserved for a built-in provider", id)
		}
		if _, duplicate := registry.extensions[id]; duplicate {
			return nil, fmt.Errorf("provider extension ID %q is registered more than once", id)
		}
		registry.extensions[id] = candidate
		if source, ok := candidate.(issueprovider.SensitiveEnvironmentProvider); ok {
			for _, name := range source.SensitiveEnvironmentVariables() {
				name = strings.TrimSpace(name)
				if !envNamePattern.MatchString(name) {
					return nil, fmt.Errorf("provider extension %q declares invalid sensitive environment variable %q", id, name)
				}
				registry.sensitive[strings.ToLower(name)] = name
			}
		}
	}
	return registry, nil
}

func validProviderID(id issueprovider.ID) bool {
	return providerIDPattern.MatchString(string(id))
}

func nilProvider(candidate issueprovider.Provider) bool {
	if candidate == nil {
		return true
	}
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (r *providerRegistry) extension(id issueprovider.ID) issueprovider.Provider {
	if r == nil {
		return nil
	}
	return r.extensions[id]
}

func (r *providerRegistry) knows(id issueprovider.ID) bool {
	if _, builtIn := builtInProviderNames[id]; builtIn {
		return true
	}
	return r.extension(id) != nil
}

func (r *providerRegistry) displayName(id issueprovider.ID) string {
	if name, builtIn := builtInProviderNames[id]; builtIn {
		return name
	}
	if extension := r.extension(id); extension != nil {
		if name := strings.TrimSpace(extension.DisplayName()); name != "" {
			return name
		}
	}
	return string(id)
}

func (r *providerRegistry) sensitiveNames() []string {
	result := make([]string, 0, len(r.sensitive))
	for _, name := range r.sensitive {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (r *providerRegistry) extensionNames() map[string]string {
	result := make(map[string]string, len(r.extensions))
	for id := range r.extensions {
		result[string(id)] = r.displayName(id)
	}
	return result
}
