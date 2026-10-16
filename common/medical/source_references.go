package medical

import (
	"fmt"
	"sync"
)

// sourceReferences belongs to one delegation. Only model-facing copies use aliases.
type sourceReferences struct {
	mu      sync.Mutex
	byAlias map[string]Source
	byID    map[string]string
}

func newSourceReferences() *sourceReferences {
	return &sourceReferences{byAlias: make(map[string]Source), byID: make(map[string]string)}
}

func (r *sourceReferences) register(sources []Source) []Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Source, 0, len(sources))
	for _, source := range sources {
		alias, ok := r.byID[source.ID]
		if !ok {
			alias = fmt.Sprintf("S%d", len(r.byID)+1)
			r.byID[source.ID] = alias
		}
		r.byAlias[alias] = source
		source.ID = alias
		out = append(out, source)
	}
	return out
}

func (r *sourceReferences) lookup(alias string) (Source, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source, ok := r.byAlias[alias]
	if ok {
		source.ID = alias
	}
	return source, ok
}

func (r *sourceReferences) resolve(aliases []string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		source, ok := r.byAlias[alias]
		if !ok {
			return nil, fmt.Errorf("子Agent引用了本次未检索到的来源：请使用当前委派工具返回的 S1、S2 等短编号")
		}
		ids = append(ids, source.ID)
	}
	return ids, nil
}
