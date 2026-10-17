package medical

import (
	"fmt"
	"sync"
)

// sourceReferences belongs to one delegation. Only model-facing copies use aliases.
type sourceReferences struct {
	mu           sync.Mutex
	byAlias      map[string]Source
	byID         map[string]string
	searched     bool
	searchFailed bool
}

func (r *sourceReferences) recordSearch(err error, initial bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searched = r.searched || initial
	r.searchFailed = err != nil
}

func (r *sourceReferences) needsSearch() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.searched
}

func (r *sourceReferences) validate(findings, evidence []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.searched {
		return fmt.Errorf("子Agent未执行检索：必须先调用 search_medical_knowledge")
	}
	if len(findings) > 0 && len(evidence) == 0 {
		return fmt.Errorf("子Agent结论缺少有效检索证据：无证据时 findings、evidence 必须为空数组")
	}
	return nil
}

func (r *sourceReferences) missingReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.searchFailed {
		return "检索服务失败，未获得支持结论的有效证据。"
	}
	if len(r.byAlias) == 0 {
		return "检索无结果，未获得支持结论的有效证据。"
	}
	return "已有检索资料未形成包含有效引用的医学结论。"
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
