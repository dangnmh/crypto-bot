package app_test

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/app"

	"github.com/stretchr/testify/assert"
)

type mockBot struct {
	runAsBackgroundErr error
	runErr             error
	stopErr            error
	runStarted         atomic.Bool
	runStopped         atomic.Bool
}

func (m *mockBot) RunAsBackground(_ context.Context) error {
	return m.runAsBackgroundErr
}

func (m *mockBot) Run(ctx context.Context) error {
	m.runStarted.Store(true)
	<-ctx.Done()
	m.runStopped.Store(true)
	return m.runErr
}

func (m *mockBot) Stop(_ context.Context) error {
	return m.stopErr
}

//nolint:paralleltest // signalNotify is package-level and shared, so do not run in parallel.
func TestRunBot_Lifecycle(t *testing.T) {
	// signalNotify is package-level and shared, so do not run in parallel.
	oldNotify := *app.SignalNotify
	defer func() { *app.SignalNotify = oldNotify }()

	sigChan := make(chan chan<- os.Signal, 1)
	*app.SignalNotify = func(c chan<- os.Signal, sigs ...os.Signal) {
		sigChan <- c
	}

	bot := &mockBot{}
	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}

	done := make(chan struct{})
	go func() {
		err := app.RunBot(engine, bot)
		assert.NoError(t, err)
		close(done)
	}()

	var c chan<- os.Signal
	select {
	case c = <-sigChan:
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not call signalNotify")
	}

	c <- os.Interrupt

	select {
	case <-done:
		assert.True(t, bot.runStarted.Load())
		assert.True(t, bot.runStopped.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not exit after simulated signal")
	}
}

func TestRunBot_BackgroundFail(t *testing.T) {
	t.Parallel()
	bot := &mockBot{
		runAsBackgroundErr: fmt.Errorf("background fail"),
	}
	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}

	err := app.RunBot(engine, bot)
	assert.Error(t, err)
	assert.Equal(t, "background fail", err.Error())
	assert.False(t, bot.runStarted.Load())
}

//nolint:paralleltest // signalNotify is package-level and shared, so do not run in parallel.
func TestRunBot_StopError(t *testing.T) {
	// signalNotify is package-level and shared, so do not run in parallel.
	oldNotify := *app.SignalNotify
	defer func() { *app.SignalNotify = oldNotify }()

	sigChan := make(chan chan<- os.Signal, 1)
	*app.SignalNotify = func(c chan<- os.Signal, sigs ...os.Signal) {
		sigChan <- c
	}

	bot := &mockBot{
		stopErr: fmt.Errorf("stop failed"),
	}
	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}

	done := make(chan struct{})
	go func() {
		_ = app.RunBot(engine, bot)
		close(done)
	}()

	var c chan<- os.Signal
	select {
	case c = <-sigChan:
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not call signalNotify")
	}

	c <- os.Interrupt

	select {
	case <-done:
		assert.True(t, bot.runStarted.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not exit after simulated signal")
	}
}
