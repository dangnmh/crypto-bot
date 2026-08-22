package ws

import (
	"context"
	"log/slog"
	"sync"
)

// Pool manages multiple WebSocket clients to bypass subscription limits (e.g., 30 pairs/conn).
type Pool struct {
	publicURL      string
	privateURL     string
	maxPairs       int
	logger         *slog.Logger
	publicOptions  []ClientOption
	privateOptions []ClientOption

	privateClient *Client
	privateMsgs   []any

	mu                  sync.RWMutex
	publicClients       []*Client
	publicClientTargets []string
	clientSubCount      []int
	topicRouting        map[string]int // topic -> public client index
	subscriptions       map[string]publicSubscription

	handlerMu sync.RWMutex
	handlers  map[string][]Handler
}

type publicSubscription struct {
	topic          string
	subscribeMsg   any
	unsubscribeMsg any
	clientIdx      int
}

// NewPool creates a new generic WS connection pool.
func NewPool(wsURL string, maxPairs int, logger *slog.Logger, opts ...ClientOption) *Pool {
	return NewPoolWithURLs(wsURL, wsURL, maxPairs, logger, opts, opts)
}

// NewPoolWithURLs creates a pool with separate public and private endpoints.
func NewPoolWithURLs(
	publicURL string,
	privateURL string,
	maxPairs int,
	logger *slog.Logger,
	publicOpts []ClientOption,
	privateOpts []ClientOption,
) *Pool {
	return &Pool{
		publicURL:      publicURL,
		privateURL:     privateURL,
		maxPairs:       maxPairs,
		handlers:       make(map[string][]Handler),
		logger:         logger,
		publicOptions:  publicOpts,
		privateOptions: privateOpts,
		topicRouting:   make(map[string]int),
		subscriptions:  make(map[string]publicSubscription),
	}
}

// Connect initiates the primary authenticated connection.
func (p *Pool) Connect(ctx context.Context) {
	p.mu.Lock()
	if p.privateClient == nil {
		opts := append([]ClientOption{}, p.privateOptions...)
		opts = append(opts, WithOnReady(func(c *Client) {
			p.replayPrivateSubscriptions(c)
		}))
		p.privateClient = NewClient(p.privateURL, p.logger, opts...)
		p.attachHandlers(p.privateClient)
		go p.privateClient.Connect(ctx)
	}
	p.mu.Unlock()
}

// On registers a callback for a specific WebSocket channel.
func (p *Pool) On(channel string, handler Handler) {
	p.handlerMu.Lock()
	p.handlers[channel] = append(p.handlers[channel], handler)
	p.handlerMu.Unlock()

	// Push the new handler down to all existing clients.
	// Since Client.OnMessage supports 1 handler per channel, we should wrap them if there are multiple.
	// But it's easier to just rebuild a single broadcast func per channel and pass it.

	p.mu.RLock()
	defer p.mu.RUnlock()

	mergedHandler := func(data []byte) {
		p.handlerMu.RLock()
		cbs := p.handlers[channel]
		p.handlerMu.RUnlock()
		for _, cb := range cbs {
			cb(data)
		}
	}

	if p.privateClient != nil {
		p.privateClient.OnMessage(channel, mergedHandler)
	}
	for _, pc := range p.publicClients {
		pc.OnMessage(channel, mergedHandler)
	}
}

// attachHandlers pushes all registered handlers to a new child client.
func (p *Pool) attachHandlers(client *Client) {
	p.handlerMu.RLock()
	defer p.handlerMu.RUnlock()
	for channel := range p.handlers {
		ch := channel // capture loop var
		client.OnMessage(ch, func(data []byte) {
			p.handlerMu.RLock()
			cbs := p.handlers[ch]
			p.handlerMu.RUnlock()
			for _, cb := range cbs {
				cb(data)
			}
		})
	}
}

// WaitReady blocks until the primary connection is authenticated and ready.
func (p *Pool) WaitReady(ctx context.Context) error {
	p.mu.RLock()
	pc := p.privateClient
	p.mu.RUnlock()
	if pc != nil {
		return pc.WaitReady(ctx)
	}
	return nil
}

// Close gracefully shuts down all connections.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.privateClient != nil {
		p.privateClient.Close()
	}

	for _, pc := range p.publicClients {
		pc.Close()
	}
}

// getOrCreatePublicClientIdx returns the index of an available public client or allocates a new one.
// Returns (clientIndex, isNewlySpawned, client).
// Callers MUST hold p.mu.Lock(). This method does NOT block on network connection or dial.
func (p *Pool) getOrCreatePublicClientIdx(targetURL string) (int, bool, *Client) {
	if targetURL == "" {
		targetURL = p.publicURL
	}
	for i, count := range p.clientSubCount {
		if count < p.maxPairs && (i < len(p.publicClientTargets) && p.publicClientTargets[i] == targetURL) {
			return i, false, p.publicClients[i]
		}
	}

	idx := len(p.publicClients)
	opts := append([]ClientOption{}, p.publicOptions...)
	opts = append(opts, WithOnReady(func(c *Client) {
		p.replayPublicSubscriptions(idx, c)
	}))
	newClient := NewClient(targetURL, p.logger, opts...)
	p.attachHandlers(newClient)
	p.publicClients = append(p.publicClients, newClient)
	p.publicClientTargets = append(p.publicClientTargets, targetURL)
	p.clientSubCount = append(p.clientSubCount, 0)

	return idx, true, newClient
}

func (p *Pool) replayPublicSubscriptions(idx int, client *Client) {
	p.mu.RLock()
	subs := make([]publicSubscription, 0, len(p.subscriptions))
	for _, sub := range p.subscriptions {
		if sub.clientIdx == idx {
			subs = append(subs, sub)
		}
	}
	p.mu.RUnlock()

	for _, sub := range subs {
		if err := client.SendJSON(sub.subscribeMsg); err != nil {
			p.logger.Warn("🟡 WS subscription replay failed", slog.String("topic", sub.topic), slog.Any("error", err))
		}
	}
}

func (p *Pool) replayPrivateSubscriptions(client *Client) {
	p.mu.RLock()
	msgs := make([]any, len(p.privateMsgs))
	copy(msgs, p.privateMsgs)
	p.mu.RUnlock()

	for _, msg := range msgs {
		if err := client.SendJSON(msg); err != nil {
			p.logger.Warn("🟡 Private WS subscription replay failed", slog.Any("error", err))
		}
	}
}

// SubscribePublic tracks a subscription topic and routes it to an available public client.
func (p *Pool) SubscribePublic(ctx context.Context, topic string, msg any) error {
	return p.SubscribePublicWithURL(ctx, p.publicURL, topic, msg)
}

// SubscribePublicWithURL tracks a subscription topic and routes it to an available public client for the specific URL.
func (p *Pool) SubscribePublicWithURL(ctx context.Context, targetURL, topic string, msg any) error {
	if targetURL == "" {
		targetURL = p.publicURL
	}

	p.mu.Lock()
	idx, exists := p.topicRouting[topic]
	var newlySpawned bool
	var client *Client

	if !exists {
		idx, newlySpawned, client = p.getOrCreatePublicClientIdx(targetURL)
		p.topicRouting[topic] = idx
		p.clientSubCount[idx]++
		p.subscriptions[topic] = publicSubscription{
			topic:        topic,
			subscribeMsg: msg,
			clientIdx:    idx,
		}
	} else {
		sub := p.subscriptions[topic]
		sub.subscribeMsg = msg
		p.subscriptions[topic] = sub
		client = p.publicClients[idx]
	}
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		p.mu.Lock()
		if !exists {
			p.clientSubCount[idx]--
		}
		delete(p.topicRouting, topic)
		delete(p.subscriptions, topic)
		p.mu.Unlock()
		return ctx.Err()
	default:
	}

	if newlySpawned {
		p.logger.InfoContext(ctx, "🌊 Spawning new public WS connection", slog.Int("pool_idx", idx), slog.String("url", targetURL))
		go client.Connect(ctx)
		if err := client.WaitReady(ctx); err != nil {
			p.mu.Lock()
			if !exists {
				p.clientSubCount[idx]--
			}
			delete(p.topicRouting, topic)
			delete(p.subscriptions, topic)
			p.mu.Unlock()
			return err
		}
		return nil
	}

	return client.SendJSON(msg)
}

// UnsubscribePublic removes a topic tracking and routes the unsubscribe message to the correct client.
func (p *Pool) UnsubscribePublic(ctx context.Context, topic string, msg any) error {
	return p.UnsubscribePublicWithURL(ctx, topic, msg)
}

// UnsubscribePublicWithURL removes a topic tracking and routes the unsubscribe message to the correct client.
func (p *Pool) UnsubscribePublicWithURL(ctx context.Context, topic string, msg any) error {
	p.mu.Lock()
	idx, exists := p.topicRouting[topic]
	if !exists {
		p.mu.Unlock()
		return nil // Topic not tracked, nothing to unsubscribe
	}

	client := p.publicClients[idx]
	p.clientSubCount[idx]--
	sub := p.subscriptions[topic]
	sub.unsubscribeMsg = msg
	delete(p.topicRouting, topic)
	delete(p.subscriptions, topic)
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return client.SendJSON(msg)
}

// GetPrivateClient returns the primary authenticated client.
func (p *Pool) GetPrivateClient() *Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.privateClient
}

// PrivateURL returns the configured private connection URL.
func (p *Pool) PrivateURL() string {
	return p.privateURL
}

// SendPrivate routes a generic JSON payload to the private authenticated client.
func (p *Pool) SendPrivate(ctx context.Context, msg any) error {
	p.mu.Lock()
	p.privateMsgs = append(p.privateMsgs, msg)
	pc := p.privateClient
	p.mu.Unlock()

	if pc != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return pc.SendJSON(msg)
	}
	return nil
}
