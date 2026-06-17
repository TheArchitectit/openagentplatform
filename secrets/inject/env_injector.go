package inject

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// envPrefix is the namespace prefix used for all injected environment vars.
const envPrefix = "OAP_INJECTED_"

// envInjector delivers secrets as process environment variables.
type envInjector struct {
	mu     sync.Mutex
	keys   map[string]struct{} // track keys we set so cleanup can purge them
}

// newEnvInjector creates an envInjector.
func newEnvInjector() *envInjector {
	return &envInjector{keys: make(map[string]struct{})}
}

// inject writes the credential value to a process env var with the
// OAP_INJECTED_ prefix. The original spec.Value is zeroed after copy.
func (e *envInjector) inject(spec InjectionSpec) (string, error) {
	name := envPrefix + sanitizeKey(spec.Key)
	if err := os.Setenv(name, string(spec.Value)); err != nil {
		return "", fmt.Errorf("env: setenv: %w", err)
	}

	e.mu.Lock()
	e.keys[name] = struct{}{}
	e.mu.Unlock()

	return name, nil
}

// cleanup unsets the env var that was set by inject.
func (e *envInjector) cleanup(spec InjectionSpec) error {
	name := envPrefix + sanitizeKey(spec.Key)
	if err := os.Unsetenv(name); err != nil {
		return fmt.Errorf("env: unsetenv: %w", err)
	}

	e.mu.Lock()
	delete(e.keys, name)
	e.mu.Unlock()

	return nil
}

// EnvName returns the full env var name that would be used for a given key.
func EnvName(key string) string {
	return envPrefix + sanitizeKey(key)
}

// IsInjectedEnv reports whether a given env var name belongs to the
// OAP_INJECTED_ namespace.
func IsInjectedEnv(name string) bool {
	return strings.HasPrefix(name, envPrefix)
}
