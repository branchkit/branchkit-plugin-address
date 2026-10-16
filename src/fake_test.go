package main

import (
	"encoding/json"
	"fmt"
	"os"
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
	replaces    int
	refreshes   int
	typeErr     error
	// onPut and onList run before a Put or a ListAll, outside the lock, so
	// a test can make something else happen in the middle of a call.
	onPut  func(name string)
	onList func(name string)
	// listErr makes ListAll of a collection fail.
	listErr map[string]error
}

// idStrategies is each collection's id strategy as plugin.json declares it
// (or "preset:<name>" when it leaves it to the preset), so the fake refuses
// a keyed write the platform would refuse: a Put that succeeded here once
// failed on every call in the app.
var idStrategies = func() map[string]string {
	raw, err := os.ReadFile("../plugin.json")
	if err != nil {
		panic(err)
	}
	var m struct {
		Provides struct {
			Collections map[string]struct {
				Preset     string `json:"preset"`
				IDStrategy *struct {
					Type string `json:"type"`
				} `json:"id_strategy"`
			} `json:"collections"`
		} `json:"provides"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(err)
	}
	out := map[string]string{}
	for name, c := range m.Provides.Collections {
		if c.IDStrategy != nil {
			out[name] = c.IDStrategy.Type
		} else {
			out[name] = "preset:" + c.Preset
		}
	}
	return out
}()

// addressable is nil when the platform lets a record of this collection be
// written or deleted by id. The presets that leave no id (data, log) do
// not; a collection another plugin owns is not this manifest's to judge.
func addressable(name string) error {
	s, ok := idStrategies[name]
	if !ok {
		return nil
	}
	switch s {
	case "by_field", "key_value", "singleton", "preset:tag", "preset:named_entities":
		return nil
	}
	return fmt.Errorf("OPERATION_NOT_PERMITTED: %s has id_strategy %q; records are not addressable", name, s)
}

func newFakePlatform() *fakePlatform {
	return &fakePlatform{collections: map[string]map[string]json.RawMessage{}}
}

func (f *fakePlatform) ListAll(name string) ([]branchkit.CollectionRecord, error) {
	if f.onList != nil {
		f.onList(name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.listErr[name]; err != nil {
		return nil, err
	}
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
	if f.onPut != nil {
		f.onPut(name)
	}
	if err := addressable(name); err != nil {
		return err
	}
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
	if err := addressable(name); err != nil {
		return false, err
	}
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

// NativeFormatDate stands in for the platform's formatting: it marks the
// instant, so a test sees the platform was asked.
func (f *fakePlatform) NativeFormatDate(req branchkit.NativeFormatDateRequest) (*branchkit.NativeFormatDateResponse, error) {
	return &branchkit.NativeFormatDateResponse{Value: "formatted " + req.When}, nil
}

func (f *fakePlatform) SettingsRefresh() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
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
