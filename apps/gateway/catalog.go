package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

const (
	nexoraOwnedBy = "nexora"
	autoFreeModelID = "auto-free"
	autoFreeUpstreamID = "openrouter/free"
	openRouterProviderKey = "openrouter"
)

type Model struct {
	ID            string          `json:"id"`
	Object        string          `json:"object"`
	OwnedBy       string          `json:"owned_by"`
	DisplayName   string          `json:"display_name,omitempty"`
	ContextLength int             `json:"context_length,omitempty"`
	Capabilities  map[string]bool `json:"capabilities,omitempty"`
	Status        string          `json:"status,omitempty"`
	Free          bool            `json:"free"`

	// Internal routing metadata must never be serialized to API clients.
	UpstreamID  string `json:"-"`
	ProviderKey string `json:"-"`
}

type ModelCatalog struct {
	mu     sync.RWMutex
	models map[string]Model
}

func NewModelCatalog() *ModelCatalog {
	c := &ModelCatalog{models: map[string]Model{}}
	c.Upsert(Model{
		ID:             autoFreeModelID,
		Object:         "model",
		OwnedBy:        nexoraOwnedBy,
		DisplayName:    "Auto Free",
		Status:         "active",
		Free:           true,
		Capabilities:   map[string]bool{"text": true, "streaming": true},
		UpstreamID:     autoFreeUpstreamID,
		ProviderKey:    openRouterProviderKey,
	})
	return c
}

func (c *ModelCatalog) Upsert(model Model) {
	model = normalizeCatalogModel(model)
	if model.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.models[model.ID] = model
}

func (c *ModelCatalog) Replace(models []Model) {
	fresh := make(map[string]Model, len(models))
	for _, model := range models {
		model = normalizeCatalogModel(model)
		if model.ID != "" {
			fresh[model.ID] = model
		}
	}
	c.mu.Lock()
	c.models = fresh
	c.mu.Unlock()
}

func (c *ModelCatalog) Get(id string) (Model, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	model, ok := c.models[strings.TrimSpace(id)]
	return model, ok
}

func (c *ModelCatalog) ListActiveFree() []Model {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]Model, 0, len(c.models))
	for _, model := range c.models {
		if model.Status == "active" && model.Free {
			result = append(result, model)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID == autoFreeModelID {
			return true
		}
		if result[j].ID == autoFreeModelID {
			return false
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func normalizeCatalogModel(model Model) Model {
	model.ID = strings.TrimSpace(model.ID)
	model.UpstreamID = strings.TrimSpace(model.UpstreamID)
	model.ProviderKey = strings.TrimSpace(model.ProviderKey)
	if model.Object == "" {
		model.Object = "model"
	}
	// Public ownership is always Nexora; provider identity remains internal.
	model.OwnedBy = nexoraOwnedBy
	return model
}

func publicModelAlias(displayName, upstreamID string) string {
	base := slugModelName(displayName)
	if base == "" {
		base = "model"
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(upstreamID)))
	return "nexora/" + base + "-" + hex.EncodeToString(sum[:6])
}

func slugModelName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	b.Grow(len(value))
	lastDash := false

	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	result := strings.Trim(b.String(), "-")
	if len(result) > 48 {
		result = strings.Trim(result[:48], "-")
	}
	return result
}
