package observability

import "crypto-bot/pkg/tracectx"

var GenerateID = tracectx.NewID

func (c *InMemoryCollector) RLock() {
	c.mu.RLock()
}

func (c *InMemoryCollector) RUnlock() {
	c.mu.RUnlock()
}
