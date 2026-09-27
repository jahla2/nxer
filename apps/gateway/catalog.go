package main

import (
	"strings"
	"sync"
)

type Model struct {
	ID string `json:"id"`
	Object string `json:"object"`
	OwnedBy string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"`
	ContextLength int `json:"context_length,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
	Status string `json:"status,omitempty"`
	Free bool `json:"free"`
}

type ModelCatalog struct {
	mu sync.RWMutex
	models map[string]Model
}

func NewModelCatalog() *ModelCatalog {
	c := &ModelCatalog{models: map[string]Model{}}
	c.Upsert(Model{ID:"auto-free", Object:"model", OwnedBy:"nexora", DisplayName:"Auto Free", Status:"active", Free:true, Capabilities: map[string]bool{"text":true,"streaming":true}})
	return c
}

func (c *ModelCatalog) Upsert(model Model) {
	model.ID = strings.TrimSpace(model.ID)
	if model.ID == "" { return }
	c.mu.Lock()
	defer c.mu.Unlock()
	c.models[model.ID]=model
}

func (c *ModelCatalog) Replace(models []Model) {
	fresh:=make(map[string]Model,len(models))
	for _,model:=range models { model.ID=strings.TrimSpace(model.ID); if model.ID!="" { fresh[model.ID]=model } }
	c.mu.Lock()
	c.models=fresh
	c.mu.Unlock()
}

func (c *ModelCatalog) Get(id string) (Model,bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m,ok:=c.models[id]
	return m,ok
}

func (c *ModelCatalog) ListActiveFree() []Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result:=make([]Model,0,len(c.models))
	for _,m:=range c.models { if m.Status=="active" && m.Free { result=append(result,m) } }
	return result
}