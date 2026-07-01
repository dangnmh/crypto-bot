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

	mu             sync.RWMutex
	publicClients  []*Client
	clientSubCount []int
	topicRouting   map[string]int // topic -> public client index
	subscriptions  map[string]publicSubscription

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
		p.privateClient = NewClient(p.privateURL, p.logger, p.privateOptions...)
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

// getOrCreatePublicClientIdx returns the index of an available public client or creates a new one.
// The boolean return value indicates whether a new client connection was spawned.
// Callers must hold p.mu.Lock().
func (p *Pool) getOrCreatePublicClientIdx(ctx context.Context, targetURL string) (int, bool, error) {
	if targetURL == "" {
		targetURL = p.publicURL
	}
	for i, count := range p.clientSubCount {
		if count < p.maxPairs && p.publicClients[i].url == targetURL {
			return i, false, nil
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
	p.clientSubCount = append(p.clientSubCount, 0)

	p.logger.InfoContext(ctx, "🌊 Spawning new public WS connection", slog.Int("pool_idx", idx), slog.String("url", targetURL))
	go newClient.Connect(ctx)
	if err := newClient.WaitReady(ctx); err != nil {
		return 0, false, err
	}

	return idx, true, nil
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

// SubscribePublic tracks a subscription topic and routes it to an available public client.
func (p *Pool) SubscribePublic(ctx context.Context, topic string, msg any) error {
	return p.SubscribePublicWithURL(ctx, p.publicURL, topic, msg)
}

// SubscribePublicWithURL tracks a subscription topic and routes it to an available public client for the specific URL.
func (p *Pool) SubscribePublicWithURL(ctx context.Context, targetURL, topic string, msg any) error {
	p.mu.Lock()
	idx, exists := p.topicRouting[topic]
	var newlySpawned bool
	if !exists {
		var err error
		idx, newlySpawned, err = p.getOrCreatePublicClientIdx(ctx, targetURL)
		if err != nil {
			p.mu.Unlock()
			return err
		}
		p.topicRouting[topic] = idx
		p.clientSubCount[idx]++
		p.subscriptions[topic] = publicSubscription{
			topic:        topic,
			subscribeMsg: msg,
			clientIdx:    idx,
		}
	}
	client := p.publicClients[idx]
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if newlySpawned {
		// If the connection was newly spawned, replayPublicSubscriptions has already
		// sent or is about to send the subscription message on ready.
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
	p.subscriptions[topic] = sub
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
	p.mu.RLock()
	pc := p.privateClient
	p.mu.RUnlock()

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
