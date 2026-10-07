package main

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/branchkit/plugin-sdk-go"
)

// fakePlatform stands in for the platform: collections as id → payload maps,
// and a record of what was typed and shown.
type fakePlatform struct {
	mu          sync.Mutex
	collections map[string]map[string]json.RawMessage
	typed       []string
	states      []branchkit.OutputState
	cleared     int
	shown       int
	hidden      int
	replaces    int
	typeErr     error
}

func newFakePlatform() *fakePlatform {
	return &fakePlatform{collections: map[string]map[string]json.RawMessage{}}
}

func (f *fakePlatform) ListAll(name string) ([]branchkit.CollectionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.collections[name]))
	for id := range f.collections[name] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]branchkit.CollectionRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, branchkit.CollectionRecord{ID: id, Payload: f.collections[name][id]})
	}
	return out, nil
}

func (f *fakePlatform) Replace(name string, entries []branchkit.CollectionPutEntry, _ branchkit.ReplaceScope, _ ...branchkit.ReplaceOption) (branchkit.ReplaceResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaces++
	c := map[string]json.RawMessage{}
	for _, e := range entries {
		c[e.ID] = e.Payload
	}
	f.collections[name] = c
	return branchkit.ReplaceResult{}, nil
}

func (f *fakePlatform) Put(name, id string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.collections[name] == nil {
		f.collections[name] = map[string]json.RawMessage{}
	}
	f.collections[name][id] = raw
	return nil
}

func (f *fakePlatform) Delete(name, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.collections[name][id]
	delete(f.collections[name], id)
	return ok, nil
}

func (f *fakePlatform) InputTypeText(req branchkit.InputTypeTextRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.typeErr != nil {
		return f.typeErr
	}
	f.typed = append(f.typed, req.Text)
	return nil
}

func (f *fakePlatform) OutputState(req branchkit.OutputStateRequest) (*branchkit.OutputStateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, req.State)
	return &branchkit.OutputStateResponse{Ok: true}, nil
}

func (f *fakePlatform) OutputClear(branchkit.OutputClearRequest) (*branchkit.OutputClearResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	return &branchkit.OutputClearResponse{Ok: true}, nil
}

func (f *fakePlatform) HUDShow(branchkit.HUDShowRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shown++
	return nil
}

func (f *fakePlatform) HUDHide(branchkit.HUDHideRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hidden++
	return nil
}

// has reports whether a collection holds a record with this id.
func (f *fakePlatform) has(name, id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.collections[name][id]
	return ok
}

func (f *fakePlatform) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.collections[name])
}

func (f *fakePlatform) typedText() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.typed...)
}
