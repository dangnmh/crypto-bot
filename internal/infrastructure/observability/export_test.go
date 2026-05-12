package observability

var GenerateID = generateID

func (c *InMemoryCollector) RLock() {
	c.mu.RLock()
}

func (c *InMemoryCollector) RUnlock() {
	c.mu.RUnlock()
}
